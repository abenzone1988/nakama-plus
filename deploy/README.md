## 部署目录说明

### 目录结构

```text
deploy/
  README.md
  eks/
    README.md
    nakama-plus-alb.yaml
```

### 最短路径（EKS + ALB Ingress）

1. 推镜像到 ECR（tag=commit）
2. 替换 YAML 中的 `image tag / ACM cert arn / host`
3. `kubectl apply`
4. 通过 `kubectl get ingress -o wide` 拿到 ALB 地址并验证

### 生产环境落地清单（按 `build/生产环境.md`）

1. `aws configure`（region `us-east-1`）
2. `aws eks update-kubeconfig --name starhold-prod-eks --region us-east-1`
3. `kubectl get nodes`（看到 2 台节点即连通）
4. 创建 ECR 仓库（可选）：`aws ecr create-repository --repository-name starhold-us/nakama-plus ...`
5. 构建并推送镜像：`build\build-aws-ecr.bat --create-repo --also-tag-latest`
6. 获取 ACM 证书 ARN（或向运维索取），填入 `deploy/eks/nakama-plus-alb.yaml`
7. `kubectl apply -f deploy/eks/nakama-plus-alb.yaml`
8. `kubectl get ingress nakama-plus-ingress -o wide` 拿到 ALB DNS 名，发运维加 Cloudflare CNAME
9. 验证：`https://<prod-domain>/` 与 `wss://<prod-domain>/ws`；同时 `kubectl logs -f deploy/nakama-plus`

