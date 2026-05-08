## Starhold-infra（External）

> 说明：本文件为从 Word 拷贝内容整理后的 Markdown 版本。关键信息已用表格结构化，命令/示例使用代码块，便于 review 与复制执行。

### 1. EKS 集群

| 项 | 值 |
|---|---|
| 名称 | `starhold-prod-eks` |
| 区域 | `us-east-1` |
| 版本 | Kubernetes 1.35 / AL2023 / containerd 2.2 |
| 节点 | 2× `t3.large`（2 vCPU / 8 GiB），Auto Scaling 2~4 |
| Pod 网络 | VPC CNI（Pod IP = VPC IP） |
| API 端点 | `https://9D60932ADF93685E8B163DDC8CD69199.gr7.us-east-1.eks.amazonaws.com` |
| OIDC 提供者 | `oidc.eks.us-east-1.amazonaws.com/id/9D60932ADF93685E8B163DDC8CD69199` |
| Authentication mode | API（access entry 模式，不再使用 `aws-auth` ConfigMap） |

配置 `kubectl`：

```bash
aws configure                        # 填写发给你的 AccessKey
aws eks update-kubeconfig --name starhold-prod-eks --region us-east-1
kubectl get nodes -o wide
```

### 2. 网络（VPC）

| 项 | 值 |
|---|---|
| VPC | `vpc-0badc330e5b49db2f`（CIDR `10.0.0.0/16`） |
| 公网子网（用于 ALB） | `subnet-0f197b77047aa9478`（us-east-1a）、`subnet-0e6234406cb224722`（us-east-1b） |
| 私网子网（节点 / RDS） | `subnet-0616d7c3e87ac4438`（us-east-1a）、`subnet-038e48df2a0fdc1af`（us-east-1b） |
| 集群安全组 | `sg-04706507723a74d9a`（Pod / Node 出站身份） |

补充：NAT 网关已就绪，Pod 默认可访问公网。

### 3. 持久化存储（EBS / PVC）

| 项 | 值 |
|---|---|
| 默认 StorageClass | `gp3`（CSI driver `ebs.csi.aws.com`） |
| 旧 gp2 | 仍存在，但已取消默认 |
| 卷绑定模式 | `WaitForFirstConsumer` |
| 扩容 | 在线扩容已开启 |

直接在 PVC 里申请即可（无需指定 `storageClassName`）：

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: data
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 20Gi
```

### 4. 数据库（RDS PostgreSQL）

| 项 | 值 |
|---|---|
| 实例 ID | `starhold-prod-pg` |
| 端点 | `starhold-prod-pg.cne62kk8qcyy.us-east-1.rds.amazonaws.com` |
| 端口 | `5432` |
| 引擎 | PostgreSQL 16.13 |
| 规格 | `db.t3.medium` / 50 GiB `gp3` 加密 |
| 数据库名 | `starhold` |
| 主用户名 | `starhold_admin` |
| 主用户密码 | 托管在 Secrets Manager：`arn:aws:secretsmanager:us-east-1:746669197317:secret:rds!db-3e9a47b9-b758-4fce-9823-0a17a01faadd-zpiBez` |
| 备份 | 自动备份保留 7 天，窗口 18:00-19:00 UTC |
| 删除保护 | ON（删除前需先关掉） |
| 安全组 | `sg-001b0603791fccca2`（已放行：来自 `sg-04706507723a74d9a` 的 5432） |
| 公网访问 | 关闭（必须从 EKS Pod 内部连接） |

应用怎么用：直接读 K8s Secret。集群里 `default/starhold-db` 已包含完整连接信息（运维已写入）。

| Key | Value |
|---|---|
| `host` | RDS 端点 |
| `port` | `5432` |
| `username` | `starhold_admin` |
| `password` | RDS 主密码 |
| `database` | `starhold` |

Pod 环境变量示例：

```yaml
env:
  - { name: PGHOST,     valueFrom: { secretKeyRef: { name: starhold-db, key: host     }}}
  - { name: PGPORT,     valueFrom: { secretKeyRef: { name: starhold-db, key: port     }}}
  - { name: PGUSER,     valueFrom: { secretKeyRef: { name: starhold-db, key: username }}}
  - { name: PGPASSWORD, valueFrom: { secretKeyRef: { name: starhold-db, key: password }}}
  - { name: PGDATABASE, valueFrom: { secretKeyRef: { name: starhold-db, key: database }}}
```

默认部署在 `default` namespace；如要换其他 namespace，复制一份 Secret 过去即可。

临时连库做一次初始化（在集群里跑一次性 Pod）：

```bash
kubectl run pg -it --rm --restart=Never --image=postgres:16 -- bash -lc '
  PGPASSWORD=$(kubectl get secret starhold-db -o jsonpath="{.data.password}" | base64 -d)
  psql -h "$(kubectl get secret starhold-db -o jsonpath="{.data.host}" | base64 -d)" -U starhold_admin -d starhold
'
```

### 5. 公网访问 / 域名 / HTTPS

状态：已就绪。

| 项 | 值 |
|---|---|
| LB Controller | AWS Load Balancer Controller 已运行（`kube-system/aws-load-balancer-controller`，2 副本） |
| 证书 | ACM 通配符证书 `*.starhold.uk` 已签发（覆盖 `starhold.uk` 根域） |
| 证书 ARN | 向运维索取（或自行查询：`aws acm list-certificates --region us-east-1`） |
| 子网标签 | 公有 `kubernetes.io/role/elb=1`、私有 `kubernetes.io/role/internal-elb=1` 已打齐 |

业务侧标准 Ingress 模板（公网 + HTTPS）。把 `<CERT_ARN>` 替换为运维提供的 ACM ARN，`api.starhold.uk` 改为你想用的子域名：

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: api-ingress
  namespace: default
  annotations:
    alb.ingress.kubernetes.io/scheme: internet-facing
    alb.ingress.kubernetes.io/target-type: ip
    alb.ingress.kubernetes.io/listen-ports: '[{"HTTP":80},{"HTTPS":443}]'
    alb.ingress.kubernetes.io/ssl-redirect: "443"
    alb.ingress.kubernetes.io/certificate-arn: "<CERT_ARN>"
spec:
  ingressClassName: alb
  rules:
    - host: api.starhold.uk
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: starhold-api
                port:
                  number: 80
```

应用 Ingress 后约 1~2 分钟会出现一个 ALB，查看 ALB DNS：

```bash
kubectl get ingress api-ingress -o wide
# ADDRESS 列形如 k8s-default-apiingre-xxxxxxxxxx.us-east-1.elb.amazonaws.com
```

### 6. 集群内服务发现（Pod ↔ Pod、Pod ↔ DB）

| 场景 | 访问方式 |
|---|---|
| 同 namespace | `http://<service-name>:<port>` |
| 跨 namespace | `http://<service-name>.<namespace>.svc.cluster.local:<port>` |
| 数据库 | 使用 `Secret/starhold-db` 的 `host`（RDS 端点），Pod 出站会自动走集群安全组放行 |

补充：节点之间、Pod 之间默认全互通，未启用 NetworkPolicy。

### 7. 镜像仓库（ECR，开发自行创建）

建议每个服务自建一个 ECR 仓库，命名 `starhold/<service>`（如 `starhold/api`、`starhold/web`、`starhold/worker`）。

创建仓库：

```bash
aws ecr create-repository \
  --repository-name starhold/<service> \
  --image-scanning-configuration scanOnPush=true \
  --region us-east-1
```

完整镜像地址：

```text
746669197317.dkr.ecr.us-east-1.amazonaws.com/starhold/<service>:<tag>
```

推镜像登录：

```bash
aws ecr get-login-password --region us-east-1 | \
  docker login --username AWS --password-stdin 746669197317.dkr.ecr.us-east-1.amazonaws.com
```

补充：EKS 节点角色已附带 `AmazonEC2ContainerRegistryReadOnly`，节点拉镜像免密。

### 8. 监控 & 日志（默认配置）

| 项 | 状态 |
|---|---|
| CloudWatch Container Insights | 未启用（要看节点 / Pod 指标可让运维一键开） |
| metrics-server | 已装：`kubectl top nodes` / `kubectl top pods` 可直接用 |
| 应用日志 | 默认输出到 stdout/stderr，可 `kubectl logs` 查看；持久化方案（Loki / CloudWatch Logs）按需对接 |
| 节点诊断 | 节点已开 SSM，运维可远程上节点排障 |

### 9. 常用命令速查

```bash
# 看节点 / Pod
kubectl get nodes -o wide
kubectl get pods -A

# 申请存储
kubectl apply -f my-pvc.yaml          # storageClassName 留空即默认 gp3

# 部署 + 公网入口
kubectl apply -f deploy.yaml
kubectl apply -f service.yaml
kubectl apply -f ingress.yaml

# 看 Ingress / ALB
kubectl get ingress -A

# 查 Pod 日志 / 进入 Pod
kubectl logs -f <pod>
kubectl exec -it <pod> -- sh

# 看数据库连接信息（已 base64 编码，可解码）
kubectl get secret starhold-db -o jsonpath="{.data.host}" | base64 -d ; echo
```

### 10. 不要做这些操作

- 不要删除 / 修改这些 namespace 下的资源（平台组件）：`kube-system`、`kube-public`、`amazon-cloudwatch`、`external-dns`
- 不要直接修改 / 删除 IAM 角色：`AmazonEKSAutoClusterRole`、`starhold-prod-eks-node-role`、`starhold-ebs-csi-role`、`starhold-alb-controller-role`
- 不要在 RDS 控制台关闭“删除保护”或“自动备份”
- 不要把 RDS 改为公网可访问