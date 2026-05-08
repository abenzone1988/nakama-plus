# Nakama Plus 生产环境部署指南

## 前置条件

1. **配置 kubectl**
   ```powershell
   aws configure    # region 填 us-east-1
   aws eks update-kubeconfig --name starhold-prod-eks --region us-east-1
   kubectl get nodes    # 看到 2 台节点 = 通
   ```

2. **ECR 镜像已推送**
   镜像地址：`746669197317.dkr.ecr.us-east-1.amazonaws.com/starhold-us/nakama-plus:latest`

## 部署步骤

### 1. 创建命名空间
```powershell
kubectl create namespace starhold
```

### 2. 部署 etcd 集群 (3 节点)
Nakama 集群模式依赖 etcd 进行节点发现和协调。
```powershell
kubectl apply -f 00-etcd.yaml
# 等待 etcd 启动完成
kubectl get pods -n starhold -l app=etcd
```

### 3. 创建 Nakama 配置
```powershell
kubectl apply -f 00-nakama-config.yaml
```

### 4. 创建 Nakama Service
```powershell
kubectl apply -f 01-nakama-service.yaml
```

### 5. 创建 Nakama StatefulSet (3 节点)
```powershell
kubectl apply -f 02-nakama-statefulset.yaml
```

### 4. 验证部署
```powershell
# 查看 Pod 状态
kubectl get pods -n starhold -l app=nakama

# 查看日志
kubectl logs -n starhold nakama-0 -f

# 查看 Service
kubectl get svc -n starhold
```

### 5. 配置 Ingress (可选)
```powershell
kubectl apply -f 03-nakama-ingress.yaml
```

## 配置文件说明

### 00-nakama-config.yaml
- ConfigMap 存储 Nakama 主配置文件
- 使用环境变量引用数据库凭据（从 K8s Secret 读取）
- 配置了集群模式（gossip + etcd）

### 01-nakama-service.yaml
- `nakama-headless`: 无头服务，用于 StatefulSet 的稳定网络标识
- `nakama`: ClusterIP Service，用于外部访问

### 02-nakama-statefulset.yaml
- 3 个 Nakama 节点（replicas: 3）
- 每个节点使用 PVC 持久化数据
- 通过环境变量动态设置 `gossip_advertise_addr`
- 数据库连接从 K8s Secret 读取

### 03-nakama-ingress.yaml
- ALB Ingress 配置
- 需要替换证书 ARN
- 配置 HTTPS 入口

## 端口说明

| 端口 | 用途 |
|------|------|
| 7350 | Nakama Socket 连接 |
| 7351 | Console 管理界面 |
| 7340 | gRPC 接口 |
| 7335 | 集群 Gossip 通信 |

## 故障排查

```powershell
# 查看所有资源
kubectl get all -n starhold

# 查看详细事件
kubectl describe pod nakama-0 -n starhold

# 进入 Pod 调试
kubectl exec -it nakama-0 -n starhold -- /bin/sh
```
