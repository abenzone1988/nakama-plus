package server

import (
	"context"

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
	if _, found := s.templateManager.GetTplRedemption().FindByKey(giftCode); !found {
		s.logger.Info("Invalid gift code", zap.String("gift_code", in.GiftCode))
		return &game.RedeemGiftResponse{Code: 2, Msg: "Invalid gift code"}, nil
	}
	// 检查兑换码是否已经领取
	if _, exists := redeemHistory.Records[giftCode]; exists {
		s.logger.Info("Gift code already redeemed", zap.String("gift_code", in.GiftCode))
		return &game.RedeemGiftResponse{Code: 1, Msg: "Gift code already redeemed"}, nil
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

	return &game.RedeemGiftResponse{Code: 0, Msg: "Success"}, nil
}
