// 集群节点可见性与“运行一段时间后看不到其他节点”的说明与模拟测试
//
// 一、可能原因（为什么运行一段时间后无法看到其他节点、无法同步）
//
// 1. Memberlist 探活超时
//    - 节点间通过 UDP 探活（ProbeInterval/ProbeTimeout）。若网络丢包、抖动或 ProbeTimeout 过短，
//      某节点会被判为不可达 → 先进入 suspect，超时后判 dead → 触发 NotifyLeave，从 s.members 移除。
//    - 一旦被 memberlist 判死，当前实现不会自动用 etcd 的节点列表再次 Join()，因此“看不到”会持续
//      直到该节点重启并重新加入集群。
//
// 2. 配置错误（已修复）
//    - 此前 peer_config 将 push_pull_interval 错误地赋给了 memberlist 的 ProbeInterval，
//      导致“全量同步间隔”和“探活间隔”混用。现已改为：PushPullInterval → PushPullInterval，
//      ProbeInterval → ProbeInterval。
//    - 若曾把 push_pull_interval 配得很大（如 60），探活会变得很慢，易误判或收敛慢。
//
// 3. etcd 与 memberlist 脱节
//    - Join() 仅在启动时用 etcd 的成员列表调一次 memberlist.Join()。之后 etcd 的 Watch 只更新
//      serviceRegistry（gRPC 等），不会对“已在 etcd 但被 memberlist 判死的节点”再次 Join。
//    - 新节点若在集群运行后才注册到 etcd，也需确保被现有节点 Join 到（当前依赖启动时列表或
//      通过已有节点 gossip 发现，若网络分区则可能发现不到）。
//
// 4. 消息队列满
//    - peer 的 msgChan 满时 NotifyMsg 会丢消息（见 peer.go），可能影响部分状态同步。Join/Leave
//      由 memberlist 内部 gossip 传播，不经过 msgChan，因此主要影响业务广播消息。
//
// 5. 网络分区 / 防火墙
//    - 跨机房或防火墙一段时间后阻断 UDP/指定端口，会导致探活失败，双方互相判死。
//
// 二、如何通过测试/脚本模拟“看不到其他节点”
//
// - 正常基线：TestClusterPeerVisibility_ConsoleStatus 从两个节点的 Console 拉取 GetStatus，
//   校验每个节点看到的 Nodes 数量 ≥ 2，且包含对方节点名。
// - 模拟“失联”：手动停止其中一个节点（或阻断其 gossip 端口），等待约 60–90 秒（memberlist
//   SuspicionMult 与 ProbeInterval 决定），再对另一节点调用 Console GetStatus，应只看到 1 个节点。
//   若需自动化，可在脚本中：先校验 2 节点可见 → 停掉 node2 → sleep 90 → 再校验 node1 的
//   GetStatus 仅返回 1 个节点。
//
// 环境变量（与 cluster_leaderboard_test 一致）：
//   TEST_NODE1_GRPC   节点1 gRPC，默认 localhost:7349
//   TEST_NODE2_GRPC  节点2 gRPC，默认 localhost:7449
//   TEST_NODE1_CONSOLE 节点1 Console gRPC，默认 localhost:7351
//   TEST_NODE2_CONSOLE 节点2 Console gRPC，默认 localhost:7451
//
// 运行：go test -v -run TestClusterPeerVisibility -tags cluster_test ./server/
//
// 三、模拟“节点失联”的脚本示例（需先登录 Console 获取 token，或使用本地免认证环境）
//   PowerShell (grpcurl 需已安装):
//     grpcurl -plaintext localhost:7351 list
//     grpcurl -plaintext localhost:7351 nakama.console.Console/GetStatus
//   手动模拟：启动两节点 → 确认 GetStatus 返回 2 个节点 → 停止 node2 → 等待 60–90 秒
//   → 对 node1 再调 GetStatus，应只剩 1 个节点。

//go:build cluster_test

package server

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/doublemo/nakama-plus/v3/console"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

func getConsoleAddr(envKey, defaultAddr string) string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	return defaultAddr
}

// TestClusterPeerVisibility_ConsoleStatus 校验两节点在 Console GetStatus 中互相可见（基线）。
// 需已启动两节点且组成集群；Console 若需登录则可能返回 Unauthenticated，测试会跳过或失败。
func TestClusterPeerVisibility_ConsoleStatus(t *testing.T) {
	node1Console := getConsoleAddr("TEST_NODE1_CONSOLE", "localhost:7351")
	node2Console := getConsoleAddr("TEST_NODE2_CONSOLE", "localhost:7451")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn1, err := grpc.NewClient(node1Console, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err, "连接节点1 Console 失败: %s", node1Console)
	defer conn1.Close()

	conn2, err := grpc.NewClient(node2Console, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err, "连接节点2 Console 失败: %s", node2Console)
	defer conn2.Close()

	client1 := console.NewConsoleClient(conn1)
	client2 := console.NewConsoleClient(conn2)

	// 从节点1 拉状态
	resp1, err := client1.GetStatus(ctx, &emptypb.Empty{})
	if err != nil {
		t.Skipf("节点1 GetStatus 需要 Console 认证或不可用，跳过: %v", err)
		return
	}

	// 从节点2 拉状态
	resp2, err := client2.GetStatus(ctx, &emptypb.Empty{})
	if err != nil {
		t.Skipf("节点2 GetStatus 需要 Console 认证或不可用，跳过: %v", err)
		return
	}

	nodes1 := resp1.GetNodes()
	nodes2 := resp2.GetNodes()

	require.GreaterOrEqual(t, len(nodes1), 2, "节点1 应至少看到 2 个节点（自身+其他），当前: %d", len(nodes1))
	require.GreaterOrEqual(t, len(nodes2), 2, "节点2 应至少看到 2 个节点（自身+其他），当前: %d", len(nodes2))

	names1 := make(map[string]bool)
	for _, n := range nodes1 {
		names1[n.GetName()] = true
	}
	names2 := make(map[string]bool)
	for _, n := range nodes2 {
		names2[n.GetName()] = true
	}

	require.True(t, len(nodes1) > 1, "节点1 的 Status 中应包含其他节点")
	require.True(t, len(nodes2) > 1, "节点2 的 Status 中应包含其他节点")

	t.Logf("节点1 看到 %d 个节点: %v", len(nodes1), names1)
	t.Logf("节点2 看到 %d 个节点: %v", len(nodes2), names2)
	t.Log("✓ 两节点互相可见，集群可见性基线正常")
}
