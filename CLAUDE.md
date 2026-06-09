# CLAUDE.md

## 交流语言
所有回复使用中文。

## 项目说明

**nakama-plus** 是基于 Nakama 扩展的分布式游戏服务器，增加了集群支持和微服务架构能力，使用 Go 语言编写。

### 核心目录
- `apigrpc/` — gRPC/REST API 定义（Proto 文件及生成代码）
- `server/` — 服务器核心实现
- `game/` — 游戏消息定义
- `console/` — 管理后台
- `inner_module/` — 内部微服务模块

### 添加新接口规范

修改或新增接口时，必须运行根目录下的 `generate-proto.bat` 重新生成代码：

```
.\generate-proto.bat
```

流程：修改 `apigrpc/apigrpc.proto` → 运行 `generate-proto.bat` → 在 `server/` 实现接口逻辑。

不要手动编辑 `*.pb.go`、`*.pb.gw.go`、`*_grpc.pb.go` 等自动生成文件。
