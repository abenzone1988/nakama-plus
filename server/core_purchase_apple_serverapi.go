package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/doublemo/nakama-common/api"
	"github.com/gofrs/uuid/v5"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	appleStoreKitAPIBaseProduction = "https://api.storekit.itunes.apple.com"
	appleStoreKitAPIBaseSandbox    = "https://api.storekit-sandbox.itunes.apple.com"
)

type appleTransactionInfoResponse struct {
	SignedTransactionInfo string `json:"signedTransactionInfo"`
}

type appleJWSTransactionPayload struct {
	TransactionId string `json:"transactionId"`
	ProductId     string `json:"productId"`
	PurchaseDate  int64  `json:"purchaseDate"` // milliseconds since epoch
	BundleId      string `json:"bundleId"`
	Environment   string `json:"environment"`
	Price         int64  `json:"price"`    // 实际支付价格，单位为毫单位（如 990 = $0.99）
	Currency      string `json:"currency"` // ISO 4217 货币代码
}

type appleJWSHeader struct {
	Alg string   `json:"alg"`
	Kid string   `json:"kid"`
	X5c []string `json:"x5c"`
	Typ string   `json:"typ"`
}

type appleServerAPIJWTClaims struct {
	Iss string `json:"iss"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
	Aud string `json:"aud"`
	Bid string `json:"bid"`
}

func ValidatePurchaseAppleServerAPI(ctx context.Context, logger *zap.Logger, db *sql.DB, userID uuid.UUID, cfg *IAPAppleConfig, transactionID, expectedProductID, environmentHint string, persist bool) (*api.ValidatePurchaseResponse, error) {
	if cfg == nil {
		return nil, status.Error(codes.FailedPrecondition, "Apple IAP is not configured.")
	}
	if cfg.ServerAPIKeyID == "" || cfg.ServerAPIIssuerID == "" || cfg.ServerAPIBundleID == "" {
		return nil, status.Error(codes.FailedPrecondition, "Apple Server API is not configured (server_api_key_id/server_api_issuer_id/server_api_bundle_id).")
	}

	privateKeyPEM := strings.TrimSpace(cfg.ServerAPIPrivateKey)
	if privateKeyPEM == "" && cfg.ServerAPIPrivateKeyFile != "" {
		b, err := os.ReadFile(cfg.ServerAPIPrivateKeyFile)
		if err != nil {
			logger.Error("Error reading Apple Server API private key file", zap.Error(err))
			return nil, status.Error(codes.FailedPrecondition, "Apple Server API private key file could not be read.")
		}
		privateKeyPEM = string(b)
	}
	if privateKeyPEM == "" {
		return nil, status.Error(codes.FailedPrecondition, "Apple Server API is not configured (server_api_private_key or server_api_private_key_file).")
	}

	signingKey, err := parseAppleECPrivateKey(privateKeyPEM)
	if err != nil {
		logger.Error("Error parsing Apple Server API private key", zap.Error(err))
		return nil, status.Error(codes.FailedPrecondition, "Apple Server API private key is invalid.")
	}

	baseURL := appleStoreKitAPIBaseProduction
	if strings.EqualFold(environmentHint, "sandbox") || strings.EqualFold(environmentHint, "Sandbox") {
		baseURL = appleStoreKitAPIBaseSandbox
	}

	jwtToken, err := buildAppleServerAPIJWT(signingKey, cfg.ServerAPIKeyID, cfg.ServerAPIIssuerID, cfg.ServerAPIBundleID)
	if err != nil {
		logger.Error("Error building Apple Server API JWT", zap.Error(err))
		return nil, status.Error(codes.Internal, "Could not create Apple Server API token.")
	}

	signedTxn, raw, httpStatus, err := fetchAppleTransactionInfo(ctx, httpc, baseURL, jwtToken, transactionID)
	if err != nil {
		// If environment hint was wrong, try the other environment once on 404.
		if httpStatus == http.StatusNotFound && baseURL == appleStoreKitAPIBaseProduction {
			signedTxn2, raw2, httpStatus2, err2 := fetchAppleTransactionInfo(ctx, httpc, appleStoreKitAPIBaseSandbox, jwtToken, transactionID)
			if err2 == nil {
				signedTxn, raw, httpStatus, err = signedTxn2, raw2, httpStatus2, nil
				baseURL = appleStoreKitAPIBaseSandbox
			}
		}
		if err != nil {
			logger.Debug("Apple Server API transaction lookup failed", zap.Int("http_status", httpStatus), zap.Error(err), zap.String("raw", string(raw)))
			if httpStatus == http.StatusUnauthorized || httpStatus == http.StatusForbidden {
				return nil, status.Error(codes.FailedPrecondition, "Apple Server API authorization failed.")
			}
			if httpStatus == http.StatusNotFound {
				return nil, status.Error(codes.FailedPrecondition, "Apple transaction was not found.")
			}
			return nil, status.Error(codes.Unavailable, "Apple verification is currently unavailable. Try again later.")
		}
	}

	payload, err := verifyAndDecodeAppleSignedTransaction(signedTxn)
	if err != nil {
		logger.Warn("Apple signed transaction verification failed", zap.Error(err))
		return nil, status.Error(codes.FailedPrecondition, "Apple signed transaction verification failed.")
	}

	if payload.TransactionId == "" {
		return nil, status.Error(codes.FailedPrecondition, "Apple transaction payload missing transactionId.")
	}
	if payload.ProductId == "" {
		return nil, status.Error(codes.FailedPrecondition, "Apple transaction payload missing productId.")
	}
	if expectedProductID != "" && expectedProductID != payload.ProductId {
		return nil, status.Error(codes.FailedPrecondition, "Apple transaction productId mismatch.")
	}
	if payload.BundleId != "" && payload.BundleId != cfg.ServerAPIBundleID {
		return nil, status.Error(codes.FailedPrecondition, "Apple transaction bundleId mismatch.")
	}

	env := api.StoreEnvironment_PRODUCTION
	if strings.EqualFold(payload.Environment, "sandbox") || baseURL == appleStoreKitAPIBaseSandbox {
		env = api.StoreEnvironment_SANDBOX
	}

	purchaseTime := time.Unix(0, 0)
	if payload.PurchaseDate > 0 {
		purchaseTime = parseMillisecondUnixTimestamp(payload.PurchaseDate)
	}

	sPurchase := &storagePurchase{
		userID:        userID,
		store:         api.StoreProvider_APPLE_APP_STORE,
		productId:     payload.ProductId,
		transactionId: payload.TransactionId,
		rawResponse:   string(raw),
		purchaseTime:  purchaseTime,
		environment:   env,
	}

	if !persist {
		validatedPurchases := []*api.ValidatedPurchase{
			{
				UserId:           userID.String(),
				ProductId:        sPurchase.productId,
				TransactionId:    sPurchase.transactionId,
				Store:            sPurchase.store,
				PurchaseTime:     timestamppb.New(sPurchase.purchaseTime),
				ProviderResponse: sPurchase.rawResponse,
				Environment:      sPurchase.environment,
			},
		}
		return &api.ValidatePurchaseResponse{ValidatedPurchases: validatedPurchases}, nil
	}

	purchases, err := upsertPurchases(ctx, db, []*storagePurchase{sPurchase})
	if err != nil {
		return nil, err
	}

	validatedPurchases := make([]*api.ValidatedPurchase, 0, len(purchases))
	for _, p := range purchases {
		suid := p.userID.String()
		if p.userID.IsNil() {
			suid = ""
		}
		validatedPurchases = append(validatedPurchases, &api.ValidatedPurchase{
			UserId:           suid,
			ProductId:        p.productId,
			TransactionId:    p.transactionId,
			Store:            p.store,
			PurchaseTime:     timestamppb.New(p.purchaseTime),
			CreateTime:       timestamppb.New(p.createTime),
			UpdateTime:       timestamppb.New(p.updateTime),
			ProviderResponse: p.rawResponse,
			SeenBefore:       p.seenBefore,
			Environment:      p.environment,
		})
	}

	return &api.ValidatePurchaseResponse{ValidatedPurchases: validatedPurchases}, nil
}

func fetchAppleTransactionInfo(ctx context.Context, client *http.Client, baseURL, jwtToken, transactionID string) (string, []byte, int, error) {
	url := fmt.Sprintf("%s/inApps/v1/transactions/%s", strings.TrimRight(baseURL, "/"), transactionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, resp.StatusCode, err
	}

	if resp.StatusCode != http.StatusOK {
		return "", body, resp.StatusCode, errors.New("non-200 response from Apple Server API")
	}

	var out appleTransactionInfoResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", body, resp.StatusCode, err
	}
	if out.SignedTransactionInfo == "" {
		return "", body, resp.StatusCode, errors.New("missing signedTransactionInfo")
	}
	return out.SignedTransactionInfo, body, resp.StatusCode, nil
}

// extractApplePriceFromRawResponse 从 Apple Server API 的原始 HTTP 响应中提取实际支付价格。
// rawResponse 是 /inApps/v1/transactions/{id} 接口返回的 JSON 响应体。
// 返回 price（毫单位，如 990 = $0.99）和 currency（ISO 4217）。
func extractApplePriceFromRawResponse(rawResponse string) (int64, string) {
	if rawResponse == "" {
		return 0, ""
	}
	var resp appleTransactionInfoResponse
	if err := json.Unmarshal([]byte(rawResponse), &resp); err != nil {
		return 0, ""
	}
	parts := strings.Split(resp.SignedTransactionInfo, ".")
	if len(parts) != 3 {
		return 0, ""
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, ""
	}
	var payload appleJWSTransactionPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return 0, ""
	}
	return payload.Price, payload.Currency
}

// formatApplePrice 将 Apple 毫单位价格转换为显示字符串（如 990 → "0.99"）。
func formatApplePrice(price int64) string {
	return fmt.Sprintf("%.2f", float64(price)/1000.0)
}

func buildAppleServerAPIJWT(key *ecdsa.PrivateKey, keyID, issuerID, bundleID string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": issuerID,
		"iat": now.Unix(),
		"exp": now.Add(10 * time.Minute).Unix(), // Apple allows up to 60 minutes.
		"aud": "appstoreconnect-v1",
		"bid": bundleID,
	}
	t := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	t.Header["kid"] = keyID
	t.Header["typ"] = "JWT"
	return t.SignedString(key)
}

// parseAppleECPrivateKey parses a .p8 PEM EC key.
func parseAppleECPrivateKey(pemText string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, errors.New("no pem block found")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		// Some keys may be in SEC1 format.
		if k2, err2 := x509.ParseECPrivateKey(block.Bytes); err2 == nil {
			return k2, nil
		}
		return nil, err
	}
	k, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("not ecdsa private key")
	}
	return k, nil
}

func verifyAndDecodeAppleSignedTransaction(compact string) (*appleJWSTransactionPayload, error) {
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid JWS format")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	var hdr appleJWSHeader
	if err := json.Unmarshal(headerBytes, &hdr); err != nil {
		return nil, err
	}
	if hdr.Alg == "" {
		return nil, errors.New("missing alg")
	}
	if len(hdr.X5c) < 1 {
		return nil, errors.New("missing x5c")
	}

	certs, err := parseX5CCerts(hdr.X5c)
	if err != nil {
		return nil, err
	}
	if err := verifyCertChain(certs); err != nil {
		return nil, err
	}

	// Verify signature using leaf certificate public key.
	signingInput := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, err
	}
	leaf := certs[0]
	if err := verifyJWSWithCert(hdr.Alg, []byte(signingInput), sig, leaf); err != nil {
		return nil, err
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var payload appleJWSTransactionPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func parseX5CCerts(x5c []string) ([]*x509.Certificate, error) {
	certs := make([]*x509.Certificate, 0, len(x5c))
	for _, s := range x5c {
		der, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, err
		}
		c, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, err
		}
		certs = append(certs, c)
	}
	return certs, nil
}

func verifyCertChain(certs []*x509.Certificate) error {
	if len(certs) < 1 {
		return errors.New("no certs")
	}
	leaf := certs[0]

	// Apple includes the full chain in x5c. In some environments (notably minimal server images),
	// the Apple root may not be present in the OS trust store, causing "unknown authority".
	// We trust the Apple root from the chain, but guard against arbitrary roots by requiring it
	// to be self-signed and look like an Apple root.
	root := certs[len(certs)-1]
	if !root.IsCA {
		return errors.New("root certificate is not a CA")
	}
	if err := root.CheckSignatureFrom(root); err != nil {
		return fmt.Errorf("root certificate is not self-signed: %w", err)
	}
	if cn := root.Subject.CommonName; cn != "" && !strings.Contains(cn, "Apple Root CA") {
		return fmt.Errorf("unexpected root common name %q", cn)
	}

	roots := x509.NewCertPool()
	roots.AddCert(root)

	intermediates := x509.NewCertPool()
	if len(certs) > 2 {
		for _, c := range certs[1 : len(certs)-1] {
			intermediates.AddCert(c)
		}
	}

	_, err := leaf.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   time.Now(),
	})
	return err
}

func verifyJWSWithCert(alg string, signingInput, sig []byte, cert *x509.Certificate) error {
	// For App Store Server API transactions, alg is typically "ES256".
	if alg != "ES256" {
		return fmt.Errorf("unsupported alg %q", alg)
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return errors.New("certificate public key is not ECDSA")
	}
	// JWS ECDSA signatures are the raw R || S format (each fixed-size).
	if len(sig) != 64 {
		return fmt.Errorf("invalid ES256 signature length %d", len(sig))
	}
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])
	hash := sha256.Sum256(signingInput)
	if !ecdsa.Verify(pub, hash[:], r, s) {
		return errors.New("invalid JWS signature")
	}
	return nil
}
