package server

import (
	"context"
	"database/sql"
	"time"

	"github.com/doublemo/nakama-common/api"
	"github.com/doublemo/nakama-plus/v3/console"
	"github.com/doublemo/nakama-plus/v3/game"
	"github.com/doublemo/nakama-plus/v3/template"
	"github.com/gofrs/uuid/v5"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/emptypb"
)

// bootstrapStorageEntry 表示启动时需要读取的一个 storage 条目
type bootstrapStorageEntry struct {
	MapKey     string // 响应 map 中的 key 名（如 "EquipGroup", "Home"）
	Collection string // storage collection
	RecordKey  string // storage object key within the collection
}

// bootstrapStorageKeys 列出 GetLaunchBootstrapData 需要读取的 14 个 storage keys。
// 使用有序 slice（非 map）以保证 objectIDs / mapKeys / 返回值 三者顺序一致。
// 关键路径 7 个 + 延迟路径 7 个。缺失的 key 不返回，客户端走 CreateModel 兜底。
var bootstrapStorageKeys = []bootstrapStorageEntry{
	// 关键路径
	{MapKey: "EquipGroup", Collection: "EquipGroup", RecordKey: "data"},
	{MapKey: "Unlock", Collection: "Unlock", RecordKey: "data"},
	{MapKey: "Home", Collection: "Home", RecordKey: "data"},
	{MapKey: "Guide", Collection: "Guide", RecordKey: "data"},
	{MapKey: "Reconnect", Collection: "Reconnect", RecordKey: "data"},
	{MapKey: "User", Collection: "User", RecordKey: "data"},
	{MapKey: "Player", Collection: "Player", RecordKey: "data"},
	// 延迟路径
	{MapKey: "Tasks", Collection: "Tasks", RecordKey: "data"},
	{MapKey: "Bag", Collection: "Bag", RecordKey: "data"},
	{MapKey: "Activity", Collection: "Activity", RecordKey: "data"},
	{MapKey: "HomeTreasure", Collection: "HomeTreasure", RecordKey: "data"},
	{MapKey: "UnlockMonster", Collection: "UnlockMonster", RecordKey: "data"},
	{MapKey: "SubGame", Collection: "SubGame", RecordKey: "data"},
	{MapKey: "ByteGame", Collection: "ByteGame", RecordKey: "data"},
}

// getTplPayPrice 辅助函数：从模板中查询商品价格（人民币字符串）
func (s *ApiServer) getTplPayPrice(productID string) string {
	tplPays := s.templateManager.GetTplPay().FindByFilter(func(tp template.TplPay) bool {
		return tp.ID == productID
	})
	if tplPays == nil || tplPays.Len() == 0 {
		return ""
	}
	return tplPays.Get(0).Money
}

func (s *ApiServer) GetLaunchBootstrapData(ctx context.Context, in *emptypb.Empty) (*game.GetLaunchBootstrapDataResponse, error) {
	userID := ctx.Value(ctxUserIDKey{}).(uuid.UUID)
	resp := &game.GetLaunchBootstrapDataResponse{
		SchemaVersion: 1,
		Partial:       false,
	}

	// ==================== 1. Account ====================
	account, err := GetAccount(ctx, s.logger, s.db, s.statusRegistry, userID)
	if err != nil {
		s.logger.Error("bootstrap: 获取account失败", zap.Error(err), zap.String("uid", userID.String()))
		resp.Account = &game.BootstrapAccount{Code: 1, Msg: err.Error()}
		resp.Partial = true
	} else {
		createTime := ""
		if account.User != nil && account.User.CreateTime != nil {
			createTime = account.User.CreateTime.AsTime().Format(time.RFC3339)
		}
		resp.Account = &game.BootstrapAccount{
			Code:       0,
			Msg:        "ok",
			Wallet:     account.Wallet,
			Inventory:  account.Inventory,
			CreateTime: createTime,
			UserId:     userID.String(),
		}
	}

	// ==================== 2. Server Time ====================
	resp.ServerTime = time.Now().UTC().Format(time.RFC3339)

	// ==================== 3. Stamina ====================
	stamina, err := GetCurrentStamina(ctx, s.logger, s.db, s.statusRegistry)
	if err != nil {
		s.logger.Error("bootstrap: 获取stamina失败", zap.Error(err))
		resp.StaminaCode = 1
		resp.StaminaMsg = err.Error()
		resp.Partial = true
	} else {
		resp.StaminaCode = 0
		resp.StaminaMsg = "ok"
		resp.Stamina = stamina
	}

	// ==================== 4. Storage (14 keys) ====================
	resp.Storage = readBootstrapStorage(ctx, s.logger, s.db, userID)

	// ==================== 5. Equip ====================
	equipData := &EquipData{}
	if err := LoadUserData(ctx, s.logger, s.db, equipData); err != nil {
		s.logger.Error("bootstrap: 获取equip失败", zap.Error(err))
		resp.EquipCode = 1
		resp.EquipMsg = err.Error()
		resp.Partial = true
	} else {
		resp.EquipCode = 0
		resp.EquipMsg = "ok"
		resp.Equip = &game.EquipData{
			BattleEquips:           equipData.BattleEquips,
			UnlockEquips:           equipData.UnlockEquips,
			UnlockedCrystalTechIds: equipData.UnlockedCrystalTechIDs,
			CrystalSlots:           equipData.CrystalSlots,
		}
	}

	// ==================== 6. Crystal Equipments ====================
	crystalEquipmentsData := &CrystalEquipmentData{}
	if err := LoadUserData(ctx, s.logger, s.db, crystalEquipmentsData); err != nil {
		s.logger.Error("bootstrap: 获取crystal_equipments失败", zap.Error(err))
		resp.CrystalEquipments = &game.GetCrystalEquipmentsResponse{Code: 1, Msg: err.Error()}
		resp.Partial = true
	} else {
		equipments := make([]*game.CrystalEquipmentInfo, 0, len(crystalEquipmentsData.Equipments))
		for _, eq := range crystalEquipmentsData.Equipments {
			equipments = append(equipments, convertToProtoEquipment(eq))
		}
		resp.CrystalEquipments = &game.GetCrystalEquipmentsResponse{
			Code:       0,
			Msg:        "ok",
			Equipments: equipments,
		}
	}

	// ==================== 7. Crystal Skins ====================
	crystalSkinData := &CrystalSkinData{}
	if err := LoadUserData(ctx, s.logger, s.db, crystalSkinData); err != nil {
		s.logger.Error("bootstrap: 获取crystal_skins失败", zap.Error(err))
		resp.CrystalSkins = &game.GetCrystalSkinsResponse{Code: 1, Msg: err.Error()}
		resp.Partial = true
	} else {
		skins := make([]*game.CrystalSkinInfo, 0, len(crystalSkinData.Skins))
		for skinID, skin := range crystalSkinData.Skins {
			skins = append(skins, &game.CrystalSkinInfo{
				SkinId:    skinID,
				StarLevel: skin.StarLevel,
			})
		}
		resp.CrystalSkins = &game.GetCrystalSkinsResponse{
			Code:  0,
			Msg:   "ok",
			Skins: skins,
		}
	}

	// ==================== 8. Player Level ====================
	playerLevelData := &PlayerLevelData{}
	if err := LoadUserData(ctx, s.logger, s.db, playerLevelData); err != nil {
		s.logger.Error("bootstrap: 获取player_level失败", zap.Error(err))
		resp.PlayerLevelCode = 1
		resp.PlayerLevelMsg = err.Error()
		resp.Partial = true
	} else {
		resp.PlayerLevelCode = 0
		resp.PlayerLevelMsg = "ok"
		resp.PlayerLevel = &game.PlayerLevelData{
			Level:    playerLevelData.Level,
			TotalExp: playerLevelData.TotalExp,
		}
	}

	// ==================== 9. Shops ====================
	shopsOk := true
	var shopsErrMsg string

	// normal shops
	shopData := &ShopData{}
	if err := LoadUserData(ctx, s.logger, s.db, shopData); err != nil {
		s.logger.Error("bootstrap: 获取shop失败", zap.Error(err))
		shopsOk = false
		shopsErrMsg = err.Error()
	} else {
		s.initCommonShop(ctx, userID, shopData.Shops, game.ShopType_SHOP_DAILY, "Daily")
		s.initCommonShop(ctx, userID, shopData.Shops, game.ShopType_SHOP_COIN, "Coin")
		s.initCommonShop(ctx, userID, shopData.Shops, game.ShopType_SHOP_STRENGTH, "Strength")
		if err := SaveUserData(ctx, s.logger, s.db, s.metrics, s.storageIndex, shopData); err != nil {
			s.logger.Error("bootstrap: 保存shop失败", zap.Error(err))
		}
		resp.NormalShop = convertToProtoShopData(shopData)
	}

	// box shop
	boxShopData := &BoxShopData{}
	if err := LoadUserData(ctx, s.logger, s.db, boxShopData); err != nil {
		s.logger.Error("bootstrap: 获取box_shop失败", zap.Error(err))
		if shopsOk {
			shopsOk = false
			shopsErrMsg = err.Error()
		}
	} else {
		s.initBoxShopStandalone(boxShopData)
		boxShopData.RefreshFreeAdState()
		if err := SaveUserData(ctx, s.logger, s.db, s.metrics, s.storageIndex, boxShopData); err != nil {
			s.logger.Error("bootstrap: 保存box_shop失败", zap.Error(err))
		}
		resp.BoxShop = convertToProtoBoxShop(boxShopData)
	}

	// chapter shop
	chapterShopData := &ChapterShopData{}
	if err := LoadUserData(ctx, s.logger, s.db, chapterShopData); err != nil {
		s.logger.Error("bootstrap: 获取chapter_shop失败", zap.Error(err))
		if shopsOk {
			shopsOk = false
			shopsErrMsg = err.Error()
		}
	} else {
		if chapterShopData.BoughtCounts == nil {
			chapterShopData.BoughtCounts = make(map[string]int32)
		}
		if chapterShopData.ClaimedCounts == nil {
			chapterShopData.ClaimedCounts = make(map[string]int32)
		}
		resp.ChapterShop = convertToProtoChapterShop(chapterShopData, s.templateManager, s.logger)
	}

	// gem shop
	gemShopData := &GemShopData{}
	if err := LoadUserData(ctx, s.logger, s.db, gemShopData); err != nil {
		s.logger.Error("bootstrap: 获取gem_shop失败", zap.Error(err))
		if shopsOk {
			shopsOk = false
			shopsErrMsg = err.Error()
		}
	} else {
		if gemShopData.BoughtCounts == nil {
			gemShopData.BoughtCounts = make(map[string]int32)
		}
		if gemShopData.ClaimedCounts == nil {
			gemShopData.ClaimedCounts = make(map[string]int32)
		}
		resp.GemShop = convertToProtoGemShop(gemShopData, s.templateManager)
	}

	if !shopsOk {
		resp.ShopsCode = 1
		resp.ShopsMsg = shopsErrMsg
		resp.Partial = true
	}

	// ==================== 10. SignIn ====================
	signInData := &SignInData{}
	if err := LoadUserData(ctx, s.logger, s.db, signInData); err != nil {
		s.logger.Error("bootstrap: 获取sign_in失败", zap.Error(err))
		signInData.Init()
	}
	todayStr := time.Now().Format(signInDateLayout)
	isClaimedToday := signInData.LastClaimDate != "" && signInData.LastClaimDate == todayStr
	var currentDay int32
	if isClaimedToday {
		currentDay = signInData.ClaimedDay
	} else {
		currentDay = signInData.ClaimedDay + 1
		if currentDay > DailySignInTotalDays {
			currentDay = 1
		}
	}
	resp.SignInDaily = &game.GetSignInRewardResponse{
		Code:           0,
		Msg:            "ok",
		CurrentDay:     currentDay,
		IsClaimedToday: isClaimedToday,
	}

	sevenDaySignInData := &SevenDaySignInData{}
	if err := LoadUserData(ctx, s.logger, s.db, sevenDaySignInData); err != nil {
		s.logger.Error("bootstrap: 获取7day_sign_in失败", zap.Error(err))
		sevenDaySignInData.Init()
	}
	resp.SignInSevenDay = &game.GetSevenDaySignInResponse{
		Code:          0,
		Msg:           "ok",
		ClaimedDay:    sevenDaySignInData.ClaimedDay,
		LastClaimDate: sevenDaySignInData.LastClaimDate,
	}

	// ==================== 11. Payment Cards ====================

	// -- 11a. VIP --
	vipAccount, isVip, vipErr := VipAccountCheck(ctx, s.logger, s.db, userID)
	if vipErr != nil {
		s.logger.Error("bootstrap: 获取vip失败", zap.Error(vipErr))
		resp.VipCode = 1
		resp.VipMsg = vipErr.Error()
		resp.Partial = true
	} else {
		vipResponse := buildVipStatusResponse(ctx, s, userID, isVip, vipAccount)
		resp.VipCode = 0
		resp.VipMsg = "ok"
		resp.Vip = vipResponse
	}

	// -- 11b. Monthly Pass --
	monthlyCardData := &MonthlyCardData{}
	if err := LoadUserData(ctx, s.logger, s.db, monthlyCardData); err != nil {
		s.logger.Error("bootstrap: 获取monthly_card失败", zap.Error(err))
		resp.MonthlyPass = &game.GetMonthlyCardStatusResponse{Code: 1, Msg: err.Error()}
		resp.Partial = true
	} else {
		resp.MonthlyPass = buildMonthlyCardResponse(monthlyCardData, isVip, vipAccount, s)
	}

	// -- 11c. Seven Day Card --
	sevenDayData := &SevenDayData{}
	if err := LoadUserData(ctx, s.logger, s.db, sevenDayData); err != nil {
		s.logger.Error("bootstrap: 获取seven_day失败", zap.Error(err))
		resp.SevenDayCard = &game.GetSevenDayStatusResponse{Code: 1, Msg: err.Error()}
		resp.Partial = true
	} else {
		resp.SevenDayCard = buildSevenDayCardResponse(sevenDayData, s)
	}

	// -- 11d. First Charge --
	firstChargeData := &FirstChargeData{}
	if err := LoadUserData(ctx, s.logger, s.db, firstChargeData); err != nil {
		s.logger.Error("bootstrap: 获取first_charge失败", zap.Error(err))
		resp.FirstCharge = &game.GetFirstChargeStatusResponse{Code: 1, Msg: err.Error()}
		resp.Partial = true
	} else {
		resp.FirstCharge = buildFirstChargeResponse(firstChargeData, s)
	}

	return resp, nil
}

// readBootstrapStorage 批量读取 14 个 storage keys，缺失的 key 不放入 map（客户端走 CreateModel 兜底）。
func readBootstrapStorage(ctx context.Context, logger *zap.Logger, db *sql.DB, userID uuid.UUID) map[string]string {
	objectIDs := make([]*api.ReadStorageObjectId, 0, len(bootstrapStorageKeys))
	for _, entry := range bootstrapStorageKeys {
		objectIDs = append(objectIDs, &api.ReadStorageObjectId{
			Collection: entry.Collection,
			Key:        entry.RecordKey,
			UserId:     userID.String(),
		})
	}

	storageObjects, err := StorageReadObjects(ctx, logger, db, userID, objectIDs)
	if err != nil {
		logger.Error("bootstrap: 批量读取storage失败", zap.Error(err))
		return nil
	}

	// objectIDs[i] 对应 bootstrapStorageKeys[i]，也对应 storageObjects.Objects[i]
	result := make(map[string]string, len(storageObjects.Objects))
	for i, obj := range storageObjects.Objects {
		if obj != nil && obj.Value != "" && i < len(bootstrapStorageKeys) {
			result[bootstrapStorageKeys[i].MapKey] = obj.Value
		}
	}
	return result
}

// buildVipStatusResponse 构建 VIP 状态响应，复用 CheckVipStatus 核心逻辑（绕过 RPC 层）。
func buildVipStatusResponse(ctx context.Context, s *ApiServer, userID uuid.UUID, isVip bool, vipAccount *console.VipAccount) *game.CheckVipStatusResponse {
	response := &game.CheckVipStatusResponse{}

	if isVip && vipAccount.ExpiryTime != nil {
		signature, _ := GenerateVipSignature(userID.String(), vipAccount.ExpiryTime.AsTime().Unix())
		response.Signature = signature
		response.ExpireTime = vipAccount.ExpiryTime.AsTime().Format(time.RFC3339)

		vipRewardData := &VipRewardData{}
		if err := LoadUserData(ctx, s.logger, s.db, vipRewardData); err == nil {
			response.RewardClaimed = vipRewardData.RewardClaimed
		}
	}

	response.Price = s.getTplPayPrice(VipProductID)
	return response
}

// buildMonthlyCardResponse 构建月卡状态响应，复用 GetMonthlyCardStatus 核心逻辑。
func buildMonthlyCardResponse(monthlyCardData *MonthlyCardData, isVip bool, vipAccount *console.VipAccount, s *ApiServer) *game.GetMonthlyCardStatusResponse {
	var expiryTime string
	if isVip && vipAccount != nil && vipAccount.ExpiryTime != nil {
		expiryTime = vipAccount.ExpiryTime.AsTime().Format(time.RFC3339)
	}

	canClaimDailyReward := false
	if isVip {
		now := time.Now().UTC()
		if monthlyCardData.LastClaimTime.IsZero() {
			canClaimDailyReward = true
		} else {
			canClaimDailyReward = !isSameDayUTC(monthlyCardData.LastClaimTime, now)
		}
	}

	return &game.GetMonthlyCardStatusResponse{
		Code:                  0,
		Msg:                   "ok",
		IsActive:              isVip,
		ExpiryTime:            expiryTime,
		CanClaimDailyReward:   canClaimDailyReward,
		PurchaseRewardClaimed: monthlyCardData.PurchaseRewardClaimed,
		Price:                 s.getTplPayPrice(MonthlyCardID),
	}
}

// buildSevenDayCardResponse 构建七日购买状态响应。
func buildSevenDayCardResponse(sevenDayData *SevenDayData, s *ApiServer) *game.GetSevenDayStatusResponse {
	isPurchased := false
	availableDays := int32(0)
	if sevenDayData.LastPurchaseTime != "" {
		purchaseTime, err := time.Parse(time.RFC3339, sevenDayData.LastPurchaseTime)
		if err == nil {
			daysPassed := int32(time.Since(purchaseTime).Hours() / 24)
			if daysPassed < 7 {
				isPurchased = true
				availableDays = daysPassed + 1
				if availableDays > 7 {
					availableDays = 7
				}
			}
		}
	}

	return &game.GetSevenDayStatusResponse{
		Code:           0,
		Msg:            "ok",
		IsPurchased:    isPurchased,
		PurchaseTime:   sevenDayData.LastPurchaseTime,
		ClaimedDays:    sevenDayData.ClaimedDays,
		AvailableDays:  availableDays,
		TotalPurchases: sevenDayData.TotalPurchases,
		Price:          s.getTplPayPrice(SevenDayProductID),
	}
}

// buildFirstChargeResponse 构建首冲状态响应。
func buildFirstChargeResponse(firstChargeData *FirstChargeData, s *ApiServer) *game.GetFirstChargeStatusResponse {
	availableDays := int32(0)
	if firstChargeData.IsCharged {
		availableDays = calculateAvailableDays(firstChargeData.ChargeTime)
	}

	return &game.GetFirstChargeStatusResponse{
		Code:          0,
		Msg:           "ok",
		IsCharged:     firstChargeData.IsCharged,
		ChargeTime:    firstChargeData.ChargeTime,
		ClaimedDays:   firstChargeData.ClaimedDays,
		AvailableDays: availableDays,
		Price:         s.getTplPayPrice(FirstChargeProductID),
	}
}

// isSameDayUTC 判断两个 UTC 时间是否在同一天（与 IsSameDay 功能一致，但不依赖 context）
func isSameDayUTC(t1, t2 time.Time) bool {
	y1, m1, d1 := t1.Date()
	y2, m2, d2 := t2.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}
