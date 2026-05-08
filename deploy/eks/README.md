## Nakama Plus on EKS（ALB Ingress：7350/7351）

### 前置条件

- 集群：`starhold-prod-eks`（`us-east-1`）
- 数据库 Secret：`default/starhold-db` 已存在（含 `host/port/username/password/database`）
- 已安装 AWS Load Balancer Controller，且 ACM 证书可用
- ECR 仓库：`starhold-us/nakama-plus`

### 1) 创建 ECR 仓库（如未创建）

> 生产环境文档说明你们的 IAM 已包含 `ecr:CreateRepository`，可在本机直接创建。

```bash
aws ecr create-repository --repository-name starhold-us/nakama-plus --image-scanning-configuration scanOnPush=true --region us-east-1
```



（或在构建脚本里加 `--create-repo` 自动创建。）

### 2) 构建并推送镜像到 ECR（commit + latest）

在仓库根目录执行：

```bash
build\build-aws-ecr.bat --create-repo --also-tag-latest
```


执行完成会得到镜像：

```text
746669197317.dkr.ecr.us-east-1.amazonaws.com/starhold-us/nakama-plus:<commit>
746669197317.dkr.ecr.us-east-1.amazonaws.com/starhold-us/nakama-plus:latest
```

`REPLACE_WITH_COMMIT` 含义：为本仓库当前 **`git rev-parse --short HEAD`** 的值；运行 `build-aws-ecr.bat` 推送成功后，把 YAML 里镜像 tag 改成同一串字符即可。

### 3) 部署到集群

先更新 `deploy/eks/nakama-plus-alb.yaml` 里的占位：

- `alb.ingress.kubernetes.io/certificate-arn: "<ACM_CERT_ARN>"` 替换为实际证书 ARN
- `host: api.starhold.uk` 替换为你对外使用的域名

获取 ACM 证书 ARN（二选一）：

- 命令行（推荐）：

```bash
aws acm list-certificates --region us-east-1
aws acm list-certificates --region us-east-1 --query "CertificateSummaryList[?DomainName=='*.starhold.uk'].CertificateArn" --output text
```

- AWS 控制台：ACM（`us-east-1`）里找到 `*.starhold.uk`，复制 Certificate ARN

应用：

```bash
kubectl apply -f deploy/eks/nakama-plus-alb.yaml
```

查看状态：

```bash
kubectl get pods -l app=nakama-plus
kubectl logs -f deploy/nakama-plus
kubectl get ingress nakama-plus-ingress -o wide
```

### 3) 外网访问说明

- HTTP API：`https://<your-domain>/`
- WebSocket：`wss://<your-domain>/ws`

> 说明：ALB 对外只暴露 80/443，后端分别转到 Pod 的 7350（HTTP）与 7351（WS）。

DNS：生产环境文档约定为「部署后把 ALB DNS 名发运维，由他加 Cloudflare CNAME」。

```bash
kubectl get ingress nakama-plus-ingress -o wide
# 取 ADDRESS 列的 k8s-xxxx.us-east-1.elb.amazonaws.com
```

### 0) 本机如何操作 K8s（不需要一直开 AWS 后台）

在本机安装并使用命令行工具即可：

- **AWS CLI**：用于登录 AWS、拉取 kubeconfig
- **kubectl**：用于操作集群

基本流程：

```bash
aws configure
aws eks update-kubeconfig --name starhold-prod-eks --region us-east-1
kubectl get nodes -o wide
```

AWS 控制台可用于查看资源（ECR、ACM、EC2 负载均衡器等），**日常部署与排障以命令行为主**（`kubectl`、`aws`）更高效。

### 4) 以后需要对外加 7349（gRPC）可以吗？

可以，建议新增一个 `Service type=LoadBalancer` 走 NLB（TCP 7349），与 ALB 并存，不影响现有 7350/7351。

