// Copyright 2018 The Nakama Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"context"

	"github.com/doublemo/nakama-common/api"
	"github.com/doublemo/nakama-plus/v3/game"
	"github.com/doublemo/nakama-plus/v3/template"
	"github.com/gofrs/uuid/v5"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *ApiServer) ValidatePurchaseApple(ctx context.Context, in *api.ValidatePurchaseAppleRequest) (*api.ValidatePurchaseResponse, error) {
	userID := ctx.Value(ctxUserIDKey{}).(uuid.UUID)
	logger, traceID := LoggerWithTraceId(ctx, s.logger)

	// Before hook.
	if fn := s.runtime.BeforeValidatePurchaseApple(); fn != nil {
		beforeFn := func(clientIP, clientPort string) error {
			result, err, code := fn(ctx, logger, traceID, userID.String(), ctx.Value(ctxUsernameKey{}).(string), ctx.Value(ctxVarsKey{}).(map[string]string), ctx.Value(ctxExpiryKey{}).(int64), clientIP, clientPort, in)
			if err != nil {
				return status.Error(code, err.Error())
			}
			if result == nil {
				// If result is nil, requested resource is disabled.
				logger.Warn("Intercepted a disabled resource.", zap.Any("resource", ctx.Value(ctxFullMethodKey{}).(string)), zap.String("uid", userID.String()))
				return status.Error(codes.NotFound, "Requested resource was not found.")
			}
			in = result
			return nil
		}

		// Execute the before function lambda wrapped in a trace for stats measurement.
		err := traceApiBefore(ctx, logger, s.metrics, ctx.Value(ctxFullMethodKey{}).(string), beforeFn)
		if err != nil {
			return nil, err
		}
	}

	if s.config.GetIAP().Apple.SharedPassword == "" {
		return nil, status.Error(codes.FailedPrecondition, "Apple IAP is not configured.")
	}

	if len(in.Receipt) < 1 {
		return nil, status.Error(codes.InvalidArgument, "Receipt cannot be empty.")
	}

	persist := true
	if in.Persist != nil {
		persist = in.Persist.GetValue()
	}

	validation, err := ValidatePurchasesApple(ctx, logger, s.db, userID, s.config.GetIAP().Apple.SharedPassword, in.Receipt, persist)
	if err != nil {
		return nil, err
	}

	// After hook.
	if fn := s.runtime.AfterValidatePurchaseApple(); fn != nil {
		afterFn := func(clientIP, clientPort string) error {
			return fn(ctx, logger, traceID, userID.String(), ctx.Value(ctxUsernameKey{}).(string), ctx.Value(ctxVarsKey{}).(map[string]string), ctx.Value(ctxExpiryKey{}).(int64), clientIP, clientPort, validation, in)
		}

		// Execute the after function lambda wrapped in a trace for stats measurement.
		traceApiAfter(ctx, logger, s.metrics, ctx.Value(ctxFullMethodKey{}).(string), afterFn)
	}

	return validation, err
}

func (s *ApiServer) ValidatePurchaseAppleV2(ctx context.Context, in *game.ValidatePurchaseAppleV2Request) (*api.ValidatePurchaseResponse, error) {
	userID := ctx.Value(ctxUserIDKey{}).(uuid.UUID)
	logger, _ := LoggerWithTraceId(ctx, s.logger)

	if len(in.GetTransactionId()) < 1 {
		return nil, status.Error(codes.InvalidArgument, "TransactionId cannot be empty.")
	}

	persist := true
	if in.Persist != nil {
		persist = in.Persist.GetValue()
	}

	validation, err := ValidatePurchaseAppleServerAPI(ctx, logger, s.db, userID, s.config.GetIAP().Apple, in.GetTransactionId(), in.GetProductId(), in.GetEnvironment(), persist)
	if err != nil {
		return nil, err
	}

	// 发货：仅在 persist=true 时可可靠幂等（SeenBefore）。
	if persist && validation != nil && len(validation.ValidatedPurchases) > 0 {
		for _, vp := range validation.ValidatedPurchases {
			if vp == nil || vp.SeenBefore {
				continue
			}

			// Apple 返回的 productId -> 映射到 TplPay.ID（内部支付ID）
			iosProductID := vp.ProductId
			tplPays := s.templateManager.GetTplPay().FindByFilter(func(tp template.TplPay) bool {
				return tp.IOSProductID == iosProductID
			})
			if tplPays == nil || tplPays.Len() == 0 {
				logger.Warn("未找到对应的 iOSProductId 支付配置", zap.String("ios_product_id", iosProductID), zap.String("transaction_id", vp.TransactionId))
				return nil, status.Error(codes.FailedPrecondition, "iOS product is not configured.")
			}
			tplPay := tplPays.Get(0)

			// 复用现有发货逻辑：productID 使用内部支付ID（TplPay.ID）
			if err := s.deliverProductToUser(ctx, userID, tplPay.ID, tplPay.Money, nil); err != nil {
				logger.Error("Apple IAP 发货失败", zap.Error(err), zap.String("user_id", userID.String()), zap.String("pay_id", tplPay.ID), zap.String("ios_product_id", iosProductID), zap.String("transaction_id", vp.TransactionId))
				return nil, status.Error(codes.Internal, "Purchase validated but delivery failed.")
			}
		}
	}

	return validation, nil
}

func (s *ApiServer) ValidatePurchaseGoogle(ctx context.Context, in *api.ValidatePurchaseGoogleRequest) (*api.ValidatePurchaseResponse, error) {
	userID := ctx.Value(ctxUserIDKey{}).(uuid.UUID)
	logger, traceID := LoggerWithTraceId(ctx, s.logger)

	// Before hook.
	if fn := s.runtime.BeforeValidatePurchaseGoogle(); fn != nil {
		beforeFn := func(clientIP, clientPort string) error {
			result, err, code := fn(ctx, logger, traceID, userID.String(), ctx.Value(ctxUsernameKey{}).(string), ctx.Value(ctxVarsKey{}).(map[string]string), ctx.Value(ctxExpiryKey{}).(int64), clientIP, clientPort, in)
			if err != nil {
				return status.Error(code, err.Error())
			}
			if result == nil {
				// If result is nil, requested resource is disabled.
				logger.Warn("Intercepted a disabled resource.", zap.Any("resource", ctx.Value(ctxFullMethodKey{}).(string)), zap.String("uid", userID.String()))
				return status.Error(codes.NotFound, "Requested resource was not found.")
			}
			in = result
			return nil
		}

		// Execute the before function lambda wrapped in a trace for stats measurement.
		err := traceApiBefore(ctx, logger, s.metrics, ctx.Value(ctxFullMethodKey{}).(string), beforeFn)
		if err != nil {
			return nil, err
		}
	}

	if s.config.GetIAP().Google.ClientEmail == "" || s.config.GetIAP().Google.PrivateKey == "" {
		return nil, status.Error(codes.FailedPrecondition, "Google IAP is not configured.")
	}

	if len(in.Purchase) < 1 {
		return nil, status.Error(codes.InvalidArgument, "Purchase cannot be empty.")
	}

	persist := true
	if in.Persist != nil {
		persist = in.Persist.GetValue()
	}

	validation, err := ValidatePurchaseGoogle(ctx, logger, s.db, userID, s.config.GetIAP().Google, in.Purchase, persist)
	if err != nil {
		return nil, err
	}

	// After hook.
	if fn := s.runtime.AfterValidatePurchaseGoogle(); fn != nil {
		afterFn := func(clientIP, clientPort string) error {
			return fn(ctx, logger, traceID, userID.String(), ctx.Value(ctxUsernameKey{}).(string), ctx.Value(ctxVarsKey{}).(map[string]string), ctx.Value(ctxExpiryKey{}).(int64), clientIP, clientPort, validation, in)
		}

		// Execute the after function lambda wrapped in a trace for stats measurement.
		traceApiAfter(ctx, logger, s.metrics, ctx.Value(ctxFullMethodKey{}).(string), afterFn)
	}

	return validation, err
}

func (s *ApiServer) ValidatePurchaseHuawei(ctx context.Context, in *api.ValidatePurchaseHuaweiRequest) (*api.ValidatePurchaseResponse, error) {
	userID := ctx.Value(ctxUserIDKey{}).(uuid.UUID)
	logger, traceID := LoggerWithTraceId(ctx, s.logger)

	// Before hook.
	if fn := s.runtime.BeforeValidatePurchaseHuawei(); fn != nil {
		beforeFn := func(clientIP, clientPort string) error {
			result, err, code := fn(ctx, logger, traceID, userID.String(), ctx.Value(ctxUsernameKey{}).(string), ctx.Value(ctxVarsKey{}).(map[string]string), ctx.Value(ctxExpiryKey{}).(int64), clientIP, clientPort, in)
			if err != nil {
				return status.Error(code, err.Error())
			}
			if result == nil {
				// If result is nil, requested resource is disabled.
				logger.Warn("Intercepted a disabled resource.", zap.Any("resource", ctx.Value(ctxFullMethodKey{}).(string)), zap.String("uid", userID.String()))
				return status.Error(codes.NotFound, "Requested resource was not found.")
			}
			in = result
			return nil
		}

		// Execute the before function lambda wrapped in a trace for stats measurement.
		err := traceApiBefore(ctx, logger, s.metrics, ctx.Value(ctxFullMethodKey{}).(string), beforeFn)
		if err != nil {
			return nil, err
		}
	}

	if s.config.GetIAP().Huawei.PublicKey == "" ||
		s.config.GetIAP().Huawei.ClientID == "" ||
		s.config.GetIAP().Huawei.ClientSecret == "" {
		return nil, status.Error(codes.FailedPrecondition, "Huawei IAP is not configured.")
	}

	if len(in.Purchase) < 1 {
		return nil, status.Error(codes.InvalidArgument, "Purchase cannot be empty.")
	}

	if len(in.Signature) < 1 {
		return nil, status.Error(codes.InvalidArgument, "Signature cannot be empty.")
	}

	persist := true
	if in.Persist != nil {
		persist = in.Persist.GetValue()
	}

	validation, err := ValidatePurchaseHuawei(ctx, logger, s.db, userID, s.config.GetIAP().Huawei, in.Purchase, in.Signature, persist)
	if err != nil {
		return nil, err
	}

	// After hook.
	if fn := s.runtime.AfterValidatePurchaseHuawei(); fn != nil {
		afterFn := func(clientIP, clientPort string) error {
			return fn(ctx, logger, traceID, userID.String(), ctx.Value(ctxUsernameKey{}).(string), ctx.Value(ctxVarsKey{}).(map[string]string), ctx.Value(ctxExpiryKey{}).(int64), clientIP, clientPort, validation, in)
		}

		// Execute the after function lambda wrapped in a trace for stats measurement.
		traceApiAfter(ctx, logger, s.metrics, ctx.Value(ctxFullMethodKey{}).(string), afterFn)
	}

	return validation, err
}

func (s *ApiServer) ValidatePurchaseFacebookInstant(ctx context.Context, in *api.ValidatePurchaseFacebookInstantRequest) (*api.ValidatePurchaseResponse, error) {
	userID := ctx.Value(ctxUserIDKey{}).(uuid.UUID)
	logger, traceID := LoggerWithTraceId(ctx, s.logger)

	// Before hook.
	if fn := s.runtime.BeforeValidatePurchaseFacebookInstant(); fn != nil {
		beforeFn := func(clientIP, clientPort string) error {
			result, err, code := fn(ctx, logger, traceID, userID.String(), ctx.Value(ctxUsernameKey{}).(string), ctx.Value(ctxVarsKey{}).(map[string]string), ctx.Value(ctxExpiryKey{}).(int64), clientIP, clientPort, in)
			if err != nil {
				return status.Error(code, err.Error())
			}
			if result == nil {
				// If result is nil, requested resource is disabled.
				logger.Warn("Intercepted a disabled resource.", zap.Any("resource", ctx.Value(ctxFullMethodKey{}).(string)), zap.String("uid", userID.String()))
				return status.Error(codes.NotFound, "Requested resource was not found.")
			}
			in = result
			return nil
		}

		// Execute the before function lambda wrapped in a trace for stats measurement.
		err := traceApiBefore(ctx, logger, s.metrics, ctx.Value(ctxFullMethodKey{}).(string), beforeFn)
		if err != nil {
			return nil, err
		}
	}

	if s.config.GetIAP().FacebookInstant.AppSecret == "" {
		return nil, status.Error(codes.FailedPrecondition, "Facebook Instant IAP is not configured.")
	}

	if len(in.SignedRequest) < 1 {
		return nil, status.Error(codes.InvalidArgument, "SignedRequest cannot be empty.")
	}

	persist := true
	if in.Persist != nil {
		persist = in.Persist.GetValue()
	}

	validation, err := ValidatePurchaseFacebookInstant(ctx, logger, s.db, userID, s.config.GetIAP().FacebookInstant, in.SignedRequest, persist)
	if err != nil {
		return nil, err
	}

	// After hook.
	if fn := s.runtime.AfterValidatePurchaseFacebookInstant(); fn != nil {
		afterFn := func(clientIP, clientPort string) error {
			return fn(ctx, logger, traceID, userID.String(), ctx.Value(ctxUsernameKey{}).(string), ctx.Value(ctxVarsKey{}).(map[string]string), ctx.Value(ctxExpiryKey{}).(int64), clientIP, clientPort, validation, in)
		}

		// Execute the after function lambda wrapped in a trace for stats measurement.
		traceApiAfter(ctx, logger, s.metrics, ctx.Value(ctxFullMethodKey{}).(string), afterFn)
	}

	return validation, err
}
