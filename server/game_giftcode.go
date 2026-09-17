package server

import (
	"context"
	"fmt"

	"github.com/doublemo/nakama-plus/v3/game"

	"time"

	"go.uber.org/zap"
)

func (s *ApiServer) RedeemGift(ctx context.Context, in *game.RedeemGiftRequest) (*game.RedeemGiftResponse, error) {
	redeemHistory := &RedeemHistory{}
	err := LoadUserData(ctx, s.logger, s.db, redeemHistory)
	if err != nil {
		return nil, err
	}

	giftCode := in.GetGiftCode()
	redemption, found := s.templateManager.GetTplRedemption().FindByKey(giftCode)
	if !found {
		s.logger.Info("Invalid gift code", zap.String("gift_code", in.GiftCode))
		return &game.RedeemGiftResponse{Code: 2, Msg: "Invalid gift code"}, nil
	}
	// 检查兑换码是否已经领取
	if _, exists := redeemHistory.Records[giftCode]; exists {
		s.logger.Info("Gift code already redeemed", zap.String("gift_code", in.GiftCode))
		return &game.RedeemGiftResponse{Code: 1, Msg: "Gift code already redeemed"}, nil
	}

	// 获取奖励配置
	reward := GetReward(redemption.RewardID, s.templateManager.GetTplReward(), s.logger)
	if reward == nil {
		s.logger.Error("兑换码奖励配置不存在", zap.String("gift_code", giftCode), zap.String("reward_id", redemption.RewardID))
		return &game.RedeemGiftResponse{Code: 3, Msg: "Reward config not found"}, nil
	}

	// 发放奖励
	source := fmt.Sprintf("giftcode_%s", giftCode)
	walletResult, inventoryResult, err := GrantReward(ctx, s.logger, s.db, s.templateManager, s.metrics, s.storageIndex, reward, source)
	if err != nil {
		s.logger.Error("发放兑换码奖励失败", zap.Error(err), zap.String("gift_code", giftCode))
		return &game.RedeemGiftResponse{Code: 4, Msg: "Failed to grant reward"}, nil
	}

	// 添加新的领取记录
	redeemHistory.Records[giftCode] = &RedeemRecord{
		Code:       giftCode,
		RedeemTime: time.Now(),
	}

	// 保存更新后的领取历史记录
	err = SaveUserData(ctx, s.logger, s.db, s.metrics, s.storageIndex, redeemHistory)
	if err != nil {
		return nil, err
	}

	var walletUpdated *game.Wallet
	var inventoryUpdated []*game.Item
	if walletResult != nil {
		walletUpdated = walletResult.Updated
	}
	if inventoryResult != nil {
		inventoryUpdated = inventoryResult.Updated
	}

	return &game.RedeemGiftResponse{
		Code:             0,
		Msg:              "Success",
		Reward:           reward,
		WalletUpdated:    walletUpdated,
		InventoryUpdated: inventoryUpdated,
	}, nil
}
