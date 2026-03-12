package server

import (
	"context"
	"time"

	"github.com/doublemo/nakama-plus/v3/game"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *ApiServer) GetGameTime(ctx context.Context, in *emptypb.Empty) (*game.GetGameTimeResponse, error) {
	return &game.GetGameTimeResponse{GameTime: &timestamppb.Timestamp{Seconds: time.Now().Unix()}}, nil
}
