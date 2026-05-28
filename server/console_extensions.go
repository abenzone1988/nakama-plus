// Copyright 2025 The Nakama Authors
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

	"github.com/doublemo/nakama-plus/v3/console"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (s *ConsoleServer) RegisteredExtensions(ctx context.Context, in *emptypb.Empty) (*console.Extensions, error) {
	extensions := &console.Extensions{
		Satori: s.satori != nil,
	}

	return extensions, nil
}
