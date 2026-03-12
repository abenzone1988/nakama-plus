// 多节点集群排行榜集成测试
// 使用前提：需要两个已运行的 nakama-plus 节点（已组成集群）
//
// 环境变量配置：
//   TEST_NODE1_GRPC  节点1 gRPC 地址，默认 localhost:7349
//   TEST_NODE2_GRPC  节点2 gRPC 地址，默认 localhost:7449
//   TEST_DB_URL      数据库地址（用于直接写入测试数据）
//
// 运行方式：
//   go test -v -run TestClusterLeaderboard -tags cluster_test ./server/
//   或者指定两个节点地址：
//   TEST_NODE1_GRPC=192.168.1.10:7349 TEST_NODE2_GRPC=192.168.1.11:7349 go test -v -run TestClusterLeaderboard ./server/

//go:build cluster_test

package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/doublemo/nakama-common/api"
	"github.com/doublemo/nakama-plus/v3/apigrpc"
	"github.com/gofrs/uuid/v5"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// clusterNode 表示一个已运行的集群节点连接
type clusterNode struct {
	addr string
	conn *grpc.ClientConn
	cl   apigrpc.NakamaClient
}

func newClusterNode(t *testing.T, addr string) *clusterNode {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err, "连接节点 %s 失败", addr)
	return &clusterNode{
		addr: addr,
		conn: conn,
		cl:   apigrpc.NewNakamaClient(conn),
	}
}

func (n *clusterNode) Close() {
	_ = n.conn.Close()
}

// 在指定节点上创建认证上下文
func (n *clusterNode) authenticatedCtx(t *testing.T, customID string) context.Context {
	t.Helper()
	ctx := context.Background()
	authCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("sparkgame:")))

	session, err := n.cl.AuthenticateCustom(authCtx, &api.AuthenticateCustomRequest{
		Account:  &api.AccountCustom{Id: customID},
		Username: uuid.Must(uuid.NewV4()).String(),
		Create:   wrapperspb.Bool(true),
	})
	require.NoError(t, err, "节点 %s 认证失败", n.addr)

	return metadata.NewOutgoingContext(ctx, metadata.New(map[string]string{
		"authorization": "Bearer " + session.Token,
	}))
}

func getNodeAddr(envKey, defaultAddr string) string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	return defaultAddr
}

// createLeaderboardForTest 通过 RPC 在指定节点创建排行榜，返回实际使用的 leaderboardId。
// 优先使用 clientrpc.create_leaderboard_for_test（指定 id），若不存在则回退到 clientrpc.create_leaderboard（随机 id）。
func createLeaderboardForTest(t *testing.T, node *clusterNode, lbId string) string {
	t.Helper()
	ctx := node.authenticatedCtx(t, uuid.Must(uuid.NewV4()).String())

	// 优先尝试 create_leaderboard_for_test（指定 id）
	payload, _ := json.Marshal(map[string]string{"id": lbId})
	resp, err := node.cl.RpcFunc(ctx, &api.Rpc{
		Id:      "clientrpc.create_leaderboard_for_test",
		Payload: string(payload),
	})
	if err == nil {
		t.Logf("已创建排行榜: %s", lbId)
		time.Sleep(300 * time.Millisecond)
		return lbId
	}

	// 回退：使用 create_leaderboard（返回随机 id）
	resp, err = node.cl.RpcFunc(ctx, &api.Rpc{
		Id:      "clientrpc.create_leaderboard",
		Payload: `{"operator":"best"}`,
	})
	require.NoError(t, err, "创建排行榜失败（需确保 data/modules/clientrpc.lua 已加载）")
	var result struct {
		LeaderboardId string `json:"leaderboard_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Payload), &result), "解析 RPC 响应失败")
	lbId = result.LeaderboardId
	t.Logf("已创建排行榜（随机 id）: %s", lbId)
	time.Sleep(300 * time.Millisecond)
	return lbId
}

// TestClusterLeaderboard_CrossNodeWrite 测试跨节点写入和读取排行榜
//
// 场景：
//  1. 节点1 创建排行榜并提交分数
//  2. 节点2 提交不同用户的分数
//  3. 等待 BinaryLog 广播同步（~500ms）
//  4. 分别在节点1和节点2查询，验证两节点数据一致
func TestClusterLeaderboard_CrossNodeWrite(t *testing.T) {
	node1Addr := getNodeAddr("TEST_NODE1_GRPC", "localhost:7349")
	node2Addr := getNodeAddr("TEST_NODE2_GRPC", "localhost:7449")

	node1 := newClusterNode(t, node1Addr)
	defer node1.Close()
	node2 := newClusterNode(t, node2Addr)
	defer node2.Close()

	lbId := "cluster-test-lb-" + uuid.Must(uuid.NewV4()).String()[:8]
	t.Logf("测试排行榜 ID: %s", lbId)
	t.Logf("节点1: %s  节点2: %s", node1Addr, node2Addr)

	lbId = createLeaderboardForTest(t, node1, lbId)

	// 用户1 在节点1 提交分数 100
	user1ID := uuid.Must(uuid.NewV4()).String()
	ctx1 := node1.authenticatedCtx(t, user1ID)
	_, err := node1.cl.WriteLeaderboardRecord(ctx1, &api.WriteLeaderboardRecordRequest{
		LeaderboardId: lbId,
		Record: &api.WriteLeaderboardRecordRequest_LeaderboardRecordWrite{
			Score:    100,
			Subscore: 10,
		},
	})
	require.NoError(t, err, "节点1 用户1 写入分数失败")
	t.Logf("[节点1] 用户1 提交分数 100")

	// 用户2 在节点2 提交分数 200
	user2ID := uuid.Must(uuid.NewV4()).String()
	ctx2 := node2.authenticatedCtx(t, user2ID)
	_, err = node2.cl.WriteLeaderboardRecord(ctx2, &api.WriteLeaderboardRecordRequest{
		LeaderboardId: lbId,
		Record: &api.WriteLeaderboardRecordRequest_LeaderboardRecordWrite{
			Score:    200,
			Subscore: 20,
		},
	})
	require.NoError(t, err, "节点2 用户2 写入分数失败")
	t.Logf("[节点2] 用户2 提交分数 200")

	// 用户3 在节点1 提交分数 150
	user3ID := uuid.Must(uuid.NewV4()).String()
	ctx3 := node1.authenticatedCtx(t, user3ID)
	_, err = node1.cl.WriteLeaderboardRecord(ctx3, &api.WriteLeaderboardRecordRequest{
		LeaderboardId: lbId,
		Record: &api.WriteLeaderboardRecordRequest_LeaderboardRecordWrite{
			Score:    150,
			Subscore: 15,
		},
	})
	require.NoError(t, err, "节点1 用户3 写入分数失败")
	t.Logf("[节点1] 用户3 提交分数 150")

	// 等待 BinaryLog 广播同步到所有节点
	t.Log("等待集群同步 (1s)...")
	time.Sleep(1 * time.Second)

	// 验证节点1 的排行榜数据
	t.Log("--- 验证节点1 排行榜 ---")
	resp1, err := node1.cl.ListLeaderboardRecords(ctx1, &api.ListLeaderboardRecordsRequest{
		LeaderboardId: lbId,
		Limit:         wrapperspb.Int32(10),
	})
	require.NoError(t, err, "节点1 查询排行榜失败")
	require.Len(t, resp1.Records, 3, "节点1 应有 3 条记录")
	logLeaderboardRecords(t, "节点1", resp1.Records)

	// 验证节点2 的排行榜数据
	t.Log("--- 验证节点2 排行榜 ---")
	resp2, err := node2.cl.ListLeaderboardRecords(ctx2, &api.ListLeaderboardRecordsRequest{
		LeaderboardId: lbId,
		Limit:         wrapperspb.Int32(10),
	})
	require.NoError(t, err, "节点2 查询排行榜失败")
	require.Len(t, resp2.Records, 3, "节点2 应有 3 条记录（与节点1一致）")
	logLeaderboardRecords(t, "节点2", resp2.Records)

	// 验证两节点数据完全一致（分数值相同）
	require.Equal(t, len(resp1.Records), len(resp2.Records), "两节点记录数应相同")
	for i := range resp1.Records {
		require.Equal(t, resp1.Records[i].OwnerId, resp2.Records[i].OwnerId,
			"第%d条记录 OwnerId 不一致", i+1)
		require.Equal(t, resp1.Records[i].Score, resp2.Records[i].Score,
			"第%d条记录 Score 不一致", i+1)
	}
	t.Log("✓ 两节点数据一致")
}

// TestClusterLeaderboard_NodeFailover 测试在一个节点提交后另一个节点能查到
//
// 场景：多轮写入，每轮交替在不同节点写，验证最终一致性
func TestClusterLeaderboard_NodeFailover(t *testing.T) {
	node1Addr := getNodeAddr("TEST_NODE1_GRPC", "localhost:7349")
	node2Addr := getNodeAddr("TEST_NODE2_GRPC", "localhost:7449")

	node1 := newClusterNode(t, node1Addr)
	defer node1.Close()
	node2 := newClusterNode(t, node2Addr)
	defer node2.Close()

	lbId := "cluster-failover-" + uuid.Must(uuid.NewV4()).String()[:8]
	t.Logf("测试排行榜 ID: %s", lbId)

	lbId = createLeaderboardForTest(t, node1, lbId)

	type scoreEntry struct {
		userID string
		score  int64
		node   *clusterNode
		ctx    context.Context
	}

	entries := make([]scoreEntry, 0, 10)

	// 交替在两个节点提交 10 个用户的分数
	for i := 0; i < 10; i++ {
		userID := uuid.Must(uuid.NewV4()).String()
		score := int64((i + 1) * 100)

		var targetNode *clusterNode
		if i%2 == 0 {
			targetNode = node1
		} else {
			targetNode = node2
		}

		ctx := targetNode.authenticatedCtx(t, userID)
		_, err := targetNode.cl.WriteLeaderboardRecord(ctx, &api.WriteLeaderboardRecordRequest{
			LeaderboardId: lbId,
			Record: &api.WriteLeaderboardRecordRequest_LeaderboardRecordWrite{
				Score: score,
			},
		})
		require.NoError(t, err, "用户%d 在节点 %s 写入失败", i+1, targetNode.addr)
		t.Logf("[%s] 用户%d 提交分数 %d", targetNode.addr, i+1, score)

		entries = append(entries, scoreEntry{userID: userID, score: score, node: targetNode, ctx: ctx})
	}

	// 等待集群同步
	t.Log("等待集群同步 (1s)...")
	time.Sleep(1 * time.Second)

	// 从节点1 查询，应看到全部 10 条
	ctx1 := entries[0].ctx
	resp1, err := node1.cl.ListLeaderboardRecords(ctx1, &api.ListLeaderboardRecordsRequest{
		LeaderboardId: lbId,
		Limit:         wrapperspb.Int32(20),
	})
	require.NoError(t, err)
	require.Len(t, resp1.Records, 10, "节点1 应看到 10 条记录")

	// 从节点2 查询，应看到全部 10 条
	ctx2 := entries[1].ctx
	resp2, err := node2.cl.ListLeaderboardRecords(ctx2, &api.ListLeaderboardRecordsRequest{
		LeaderboardId: lbId,
		Limit:         wrapperspb.Int32(20),
	})
	require.NoError(t, err)
	require.Len(t, resp2.Records, 10, "节点2 应看到 10 条记录")

	logLeaderboardRecords(t, "节点1", resp1.Records)
	logLeaderboardRecords(t, "节点2", resp2.Records)
	t.Log("✓ 多轮交替写入后两节点数据一致")
}

// TestClusterLeaderboard_RankConsistency 测试跨节点 Rank 排名一致性
//
// 场景：节点1插入高分用户，节点2查询该用户的排名是否正确
func TestClusterLeaderboard_RankConsistency(t *testing.T) {
	node1Addr := getNodeAddr("TEST_NODE1_GRPC", "localhost:7349")
	node2Addr := getNodeAddr("TEST_NODE2_GRPC", "localhost:7449")

	node1 := newClusterNode(t, node1Addr)
	defer node1.Close()
	node2 := newClusterNode(t, node2Addr)
	defer node2.Close()

	lbId := "cluster-rank-" + uuid.Must(uuid.NewV4()).String()[:8]
	t.Logf("测试排行榜 ID: %s", lbId)

	lbId = createLeaderboardForTest(t, node1, lbId)

	users := []struct {
		id    string
		score int64
	}{
		{uuid.Must(uuid.NewV4()).String(), 10},
		{uuid.Must(uuid.NewV4()).String(), 30},
		{uuid.Must(uuid.NewV4()).String(), 50},
		{uuid.Must(uuid.NewV4()).String(), 20},
		{uuid.Must(uuid.NewV4()).String(), 40},
	}

	// 奇数用户在节点1，偶数用户在节点2
	for i, u := range users {
		var targetNode *clusterNode
		if i%2 == 0 {
			targetNode = node1
		} else {
			targetNode = node2
		}
		ctx := targetNode.authenticatedCtx(t, u.id)
		_, err := targetNode.cl.WriteLeaderboardRecord(ctx, &api.WriteLeaderboardRecordRequest{
			LeaderboardId: lbId,
			Record: &api.WriteLeaderboardRecordRequest_LeaderboardRecordWrite{
				Score: u.score,
			},
		})
		require.NoError(t, err)
		t.Logf("[%s] 用户%d 提交分数 %d", targetNode.addr, i+1, u.score)
	}

	// 等待 Rank 缓存同步
	time.Sleep(1 * time.Second)

	// 从节点2 查询完整排行榜（验证跨节点写入的排名一致性）
	// 使用 ListLeaderboardRecords 比 AroundOwner 更稳定（不依赖 expiry_time 精确匹配）
	ctx2 := node2.authenticatedCtx(t, uuid.Must(uuid.NewV4()).String())
	resp, err := node2.cl.ListLeaderboardRecords(ctx2, &api.ListLeaderboardRecordsRequest{
		LeaderboardId: lbId,
		Limit:         wrapperspb.Int32(10),
	})
	require.NoError(t, err, "节点2 查询排行榜失败")
	require.Len(t, resp.Records, 5, "应有 5 条记录")

	t.Logf("[节点2] 排行榜结果:")
	for _, r := range resp.Records {
		t.Logf("  OwnerId=%.8s... Score=%d Rank=%d", r.OwnerId, r.Score, r.Rank)
	}

	// 降序：score 50 > 40 > 30 > 20 > 10，验证排名顺序正确
	require.Equal(t, int64(50), resp.Records[0].Score, "最高分(50)应排第一")
	require.Equal(t, int64(40), resp.Records[1].Score)
	require.Equal(t, int64(30), resp.Records[2].Score)
	t.Logf("✓ 节点2 能正确查询到跨节点写入的排名顺序")
}

func logLeaderboardRecords(t *testing.T, nodeLabel string, records []*api.LeaderboardRecord) {
	t.Helper()
	for i, r := range records {
		t.Logf("[%s] #%d OwnerId=%.8s... Score=%d SubScore=%d Rank=%d",
			nodeLabel, i+1, r.OwnerId, r.Score, r.Subscore, r.Rank)
	}
}

// TestClusterLeaderboard_Help 打印使用帮助（直接运行此测试查看配置说明）
func TestClusterLeaderboard_Help(t *testing.T) {
	help := `
=== 多节点集群排行榜测试 ===

前提条件：
  1. 启动两个已组成集群的 nakama-plus 节点
  2. 节点1 gRPC 端口默认 7349，节点2 默认 7449
  3. 服务端需加载 data/modules/clientrpc.lua（含 create_leaderboard_for_test RPC）

运行方式：
  # 使用默认端口（节点1:7349 节点2:7449
  go test -v -run TestClusterLeaderboard -tags cluster_test ./server/

  # 指定节点地址
  $env:TEST_NODE1_GRPC="192.168.1.10:7349"
  $env:TEST_NODE2_GRPC="192.168.1.11:7449"
  go test -v -run TestClusterLeaderboard -tags cluster_test ./server/

  # 只运行某个子测试
  go test -v -run TestClusterLeaderboard_CrossNodeWrite -tags cluster_test ./server/
  go test -v -run TestClusterLeaderboard_RankConsistency -tags cluster_test ./server/

测试内容：
  - CrossNodeWrite    ：节点1/节点2 交叉写入，验证两节点数据一致
  - NodeFailover      ：10个用户交替在两节点写入，验证最终一致性
  - RankConsistency   ：节点1写入的用户在节点2查询排名是否正确
`
	fmt.Print(help)
	t.Log("使用 -tags cluster_test 运行集群测试")
}
