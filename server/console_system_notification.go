// Copyright 2019 The Nakama Authors
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
	"encoding/json"
	"time"

	"github.com/doublemo/nakama-plus/v3/console"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *ConsoleServer) ListSystemNotifications(ctx context.Context, in *console.ListSystemNoticeRequest) (*console.ListSystemNoticeResponse, error) {
	// 参数验证
	if in.Limit <= 0 || in.Limit > 100 {
		in.Limit = 100
	}

	notifications, err := SystemNotificationList(ctx, s.logger, s.db, int(in.Limit), in.Cursor)
	if err != nil {
		s.logger.Error("获取系统通知列表失败", zap.Error(err))
		return nil, status.Error(codes.Internal, "获取系统通知列表失败")
	}

	return notifications, nil
}

func (s *ConsoleServer) CreateSystemNotification(ctx context.Context, in *console.CreateSystemNotificationRequest) (*console.SystemNotice, error) {
	// 参数验证
	notice := in.GetNotice()
	if notice == nil {
		return nil, status.Error(codes.InvalidArgument, "通知内容不能为空")
	}
	if notice.GetSubject() == "" {
		return nil, status.Error(codes.InvalidArgument, "通知标题不能为空")
	}
	if notice.GetContent() == nil {
		return nil, status.Error(codes.InvalidArgument, "通知内容不能为空")
	}

	contentJson, err := json.Marshal(notice.GetContent())
	if err != nil {
		s.logger.Error("序列化通知内容失败", zap.Error(err))
		return nil, status.Error(codes.InvalidArgument, "通知内容格式错误")
	}

	// 根据类型处理发送逻辑
	switch in.Type {
	case 0: // 全体
		// 生效时间：空表示立即生效，使用服务器当前时间，避免控制台与服务器时差
		effectiveTime := in.Notice.GetEffectiveTime()
		if effectiveTime == nil {
			effectiveTime = timestamppb.Now()
		} else {
			now := time.Now()
			if effectiveTime.AsTime().Before(now) {
				return nil, status.Error(codes.InvalidArgument, "生效时间不能小于当前时间")
			}
		}
		s.logger.Info("创建全体系统通知", zap.String("subject", notice.GetSubject()))
		// 创建系统通知（noticeAttach 由前端以 JSONB 形式传递，当前预留为空对象）
		notification, err := SystemNotificationCreate(ctx, s.db, s.logger, notice.GetNoticeType(), notice.GetSubject(), string(contentJson), effectiveTime, notice.GetExpiryTime(), "")
		if err != nil {
			s.logger.Error("创建系统通知失败", zap.Error(err))
			return nil, status.Error(codes.Internal, "创建系统通知失败")
		}
		return notification, nil
	default:
		// 系统通知仅支持全局通知，个人通知请使用 CreatePersonalNotification 等接口
		return nil, status.Error(codes.InvalidArgument, "系统通知仅支持全局类型，个人通知请使用个人通知接口")
	}
}

func (s *ConsoleServer) DeleteSystemNotification(ctx context.Context, in *console.SystemNotificationId) (*emptypb.Empty, error) {
	// 参数验证
	if in.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "通知ID不能为空")
	}

	if err := SystemNotificationDelete(ctx, s.db, s.logger, in.GetId()); err != nil {
		if err == ErrSystemNotificationNotFound {
			return nil, status.Error(codes.NotFound, "通知不存在")
		}
		s.logger.Error("删除系统通知失败", zap.Error(err))
		return nil, status.Error(codes.Internal, "删除系统通知失败")
	}

	return &emptypb.Empty{}, nil
}

func (s *ConsoleServer) UpdateSystemNotification(ctx context.Context, in *console.SystemNotice) (*console.SystemNotice, error) {
	// 参数验证
	if in == nil {
		return nil, status.Error(codes.InvalidArgument, "请求参数不能为空")
	}
	if in.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "通知ID不能为空")
	}
	if in.GetSubject() == "" {
		return nil, status.Error(codes.InvalidArgument, "通知标题不能为空")
	}
	if in.GetContent() == nil {
		return nil, status.Error(codes.InvalidArgument, "通知内容不能为空")
	}

	contentJson, err := json.Marshal(in.GetContent())
	if err != nil {
		s.logger.Error("序列化通知内容失败", zap.Error(err))
		return nil, status.Error(codes.InvalidArgument, "通知内容格式错误")
	}

	// 生效时间为空表示立即生效，使用服务器当前时间
	effectiveTime := in.GetEffectiveTime()
	if effectiveTime == nil {
		effectiveTime = timestamppb.Now()
	}
	notification, err := SystemNotificationUpdate(ctx, s.db, s.logger, in.GetId(), in.GetSubject(), string(contentJson), effectiveTime, in.GetExpiryTime())
	if err != nil {
		if err == ErrSystemNotificationNotFound {
			return nil, status.Error(codes.NotFound, "通知不存在")
		}
		s.logger.Error("更新系统通知失败", zap.Error(err))
		return nil, status.Error(codes.Internal, "更新系统通知失败")
	}

	return notification, nil
}

func (s *ConsoleServer) GetSystemNotification(ctx context.Context, in *console.SystemNotificationId) (*console.SystemNotice, error) {
	// 参数验证
	if in.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "通知ID不能为空")
	}

	notification, err := SystemNotificationGet(ctx, s.db, s.logger, in.GetId())
	if err != nil {
		if err == ErrSystemNotificationNotFound {
			return nil, status.Error(codes.NotFound, "通知不存在")
		}
		s.logger.Error("获取系统通知失败", zap.Error(err))
		return nil, status.Error(codes.Internal, "获取系统通知失败")
	}

	return notification, nil
}
