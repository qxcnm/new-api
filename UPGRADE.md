# Docker + PostgreSQL 零停机升级

本文记录本次 `DaitouAi-Newapi` 在线升级的实际流程，适用于服务器上已有 Docker Compose、PostgreSQL、Redis 和 Nginx 的部署。

## 本次变慢的原因

这次切换耗时主要来自几个可以避免的问题：

1. 直接构建完整生产镜像时，运行时阶段需要从 Debian 软件源安装依赖，软件源返回 502，导致构建失败。
2. 为绕过构建失败，又构建了包含 Go 工具链的中间镜像，体积约 3.8GB，导出和传输都没有必要地变慢。
3. 新容器启动前没有先从 Nginx 容器网络验证上游连通性。
4. Nginx 的 `proxy_connect_timeout 0` 在当前版本表现为立即连接超时，切换后出现了短暂 504。

正确做法是先构建一个只包含运行时依赖和最终二进制的离线镜像，启动新实例并完成容器内、Nginx 网络内和公网入口三层检查，最后只执行一次 `nginx -t && nginx -s reload`。

## 升级前检查

在本地确认分支、提交和工作区范围，确保离线包包含预期代码：

```powershell
git status --short --branch
git rev-parse HEAD
git log -1 --oneline
```

服务器上确认旧服务、数据库、Redis 和 Nginx 都在运行：

```bash
docker ps --format '{{.Names}}|{{.Image}}|{{.Ports}}|{{.Status}}'
docker inspect daitouai-nginx --format '{{json .NetworkSettings.Networks}}'
```

不要在升级前执行 `docker compose down`、`down -v` 或删除旧容器。旧实例要一直保留到新实例通过公网验证。

## 1. 备份 PostgreSQL

在服务器部署目录读取现有 `.env`，不要把密码写入脚本或日志：

```bash
cd /opt/daitouai-new-api
set -a
. ./.env
set +a

stamp=$(date +%Y%m%d-%H%M%S)
mkdir -p /data/daitouai/backups
export PGPASSWORD="$POSTGRES_PASSWORD"
docker exec -e PGPASSWORD="$POSTGRES_PASSWORD" daitouai-postgres \
  pg_dump -U root -d new-api -Fc \
  > "/data/daitouai/backups/new-api-$stamp.dump"
unset PGPASSWORD
```

验证备份文件和归档目录：

```bash
ls -lh /data/daitouai/backups/new-api-*.dump
docker run --rm \
  -v /data/daitouai/backups:/backup postgres:15 \
  pg_restore -l /backup/new-api-YYYYMMDD-HHMMSS.dump | head
```

备份完成后才允许进行镜像切换。数据库容器和数据目录不重建、不删除。

## 2. 构建小型离线镜像

先构建 Go 二进制。使用已有的多阶段构建目标 `builder2`，避免把完整生产镜像的网络安装阶段作为发布阻塞点：

```powershell
docker build --target builder2 -t new-api-builder:upgrade .
New-Item -ItemType Directory -Force .\bin\upgrade-new-api | Out-Null
docker rm -f upgrade-extract 2>$null
docker create --name upgrade-extract new-api-builder:upgrade /bin/true | Out-Null
docker cp upgrade-extract:/build/new-api .\bin\upgrade-new-api\new-api
docker rm upgrade-extract
```

为运行时镜像准备一个固定的 Alpine 基础镜像，只安装证书、时区和健康检查所需的 `wget`：

将下面内容保存为 `.\bin\upgrade-new-api\Dockerfile`：

```dockerfile
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata wget
COPY new-api /new-api
RUN chmod 755 /new-api
WORKDIR /data
EXPOSE 3000
ENTRYPOINT ["/new-api"]
```

构建并导出：

```powershell
docker build -t daitouai/new-api:upgrade-YYYYMMDD .\bin\upgrade-new-api
docker save daitouai/new-api:upgrade-YYYYMMDD `
  -o .\new-api-upgrade-YYYYMMDD-HHMM.tar
Get-FileHash .\new-api-upgrade-YYYYMMDD-HHMM.tar -Algorithm SHA256
```

本次最终离线包约 46.5MB；不要上传包含 Go 编译缓存的中间镜像。

### 防止前端文件漏打包

前端源码的修改必须全部完成后再执行 `builder2`。Go 二进制会嵌入当次构建生成的 `web/dist`，之后再修改 `web/src` 不会影响已经生成的二进制。

发布前至少做一次产物核对：

```powershell
docker create --name verify-build new-api-builder:upgrade /bin/true
docker cp verify-build:/build/web/dist .\bin\verify-dist
docker rm verify-build
rg -l -F 'Sign up' .\bin\verify-dist
rg -l -F '/sign-up' .\bin\verify-dist
```

确认资源指纹来自本次构建后，再提取二进制、构建运行时镜像和导出离线包。建议每次使用新的镜像标签，不要复用旧标签或依赖旧缓存。

## 3. 上传和导入

```powershell
scp .\new-api-upgrade-YYYYMMDD-HHMM.tar root@SERVER:/tmp/
```

服务器导入并核对镜像：

```bash
docker load -i /tmp/new-api-upgrade-YYYYMMDD-HHMM.tar
docker image inspect daitouai/new-api:upgrade-YYYYMMDD \
  --format '{{.Id}} {{.Size}}'
```

## 4. 新端口并行启动

新容器使用独立端口，例如宿主机 `3001`，加入旧服务所在的 Docker 网络，复用同一个 PostgreSQL、Redis 和数据目录：

```bash
set -a
. /opt/daitouai-new-api/.env
set +a

mkdir -p /data/daitouai/logs-upgrade
docker run -d \
  --name daitouai-new-api-upgrade \
  --restart unless-stopped \
  --network daitouai-new-api_daitouai-network \
  -p 3001:3000 \
  -v /data/daitouai/new-api:/data \
  -v /data/daitouai/logs-upgrade:/app/logs \
  -e SQL_DSN="postgresql://root:${POSTGRES_PASSWORD}@postgres:5432/new-api" \
  -e REDIS_CONN_STRING="redis://:${REDIS_PASSWORD}@redis:6379" \
  -e SESSION_SECRET="$SESSION_SECRET" \
  -e TZ=Asia/Shanghai \
  -e ERROR_LOG_ENABLED=true \
  -e BATCH_UPDATE_ENABLED=true \
  -e NODE_NAME=daitouai-new-api-upgrade \
  -e SESSION_COOKIE_SECURE=false \
  daitouai/new-api:upgrade-YYYYMMDD \
  --log-dir /app/logs
```

三层检查全部通过后才切换：

```bash
# 新实例自身
curl -fsS http://127.0.0.1:3001/api/status

# Nginx 容器到新实例的 Docker DNS 和网络
docker exec daitouai-nginx \
  wget -q -O - http://daitouai-new-api-upgrade:3000/api/status

# 查看启动日志确认 PostgreSQL、Redis 和迁移均成功
docker logs --tail 80 daitouai-new-api-upgrade
```

## 5. Nginx 平滑切换和大请求配置

先备份配置，再把两个站点的上游改成新容器名。请求体大小设为 `0` 表示不限制；关闭代理缓冲，避免大图片或流式响应被 Nginx 缓冲。连接超时不要设为 `0`，使用足够长的值：

```nginx
client_max_body_size 0;
client_body_timeout 86400s;
send_timeout 86400s;

location / {
    proxy_pass http://daitouai-new-api-upgrade:3000;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto https;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $connection_upgrade;
    proxy_read_timeout 86400s;
    proxy_connect_timeout 60s;
    proxy_send_timeout 86400s;
    proxy_request_buffering off;
    proxy_buffering off;
}
```

应用配置并验证：

```bash
cp /data/nginx/new-api.conf \
  /data/nginx/new-api.conf.bak-upgrade-$(date +%Y%m%d-%H%M%S)
docker exec daitouai-nginx nginx -t
docker exec daitouai-nginx nginx -s reload
```

`nginx -s reload` 是平滑 reload，旧连接可以继续完成，新连接进入新容器。切换后从服务器验证两个域名：

```bash
curl -kfsS https://127.0.0.1/api/status -H 'Host: daitouai.com'
curl -kfsS https://127.0.0.1/api/status -H 'Host: novro.net'
```

切换耗时应单独记录，从 reload 发出开始，到所有公网入口健康检查返回 200 结束；不要把构建、上传和容器启动时间计入切换时间：

```bash
start_ms=$(date +%s%3N)
docker exec daitouai-nginx nginx -s reload
# 轮询每个域名的 /api/status，全部返回 200 后记录结束时间
end_ms=$(date +%s%3N)
echo "switch_elapsed_ms=$((end_ms-start_ms))"
```

## 回滚

如果新实例异常，恢复 Nginx 备份并 reload 即可，旧容器保持运行：

```bash
cp /data/nginx/new-api.conf.bak-upgrade-YYYYMMDD-HHMMSS \
  /data/nginx/new-api.conf
docker exec daitouai-nginx nginx -t
docker exec daitouai-nginx nginx -s reload
```

确认新版本稳定后，再单独停止并删除旧容器；不要删除 PostgreSQL 数据目录或 Redis 数据目录。

## 本次部署结果

- PostgreSQL 备份已验证为 Custom 格式，TOC 条目 391。
- 新镜像使用 `daitouai/new-api:upgrade-20260913`，新容器端口为 `3001`。
- Nginx 已切换到 `daitouai-new-api-upgrade:3000`。
- `daitouai.com`、`novro.net` 和新实例 `/api/status` 均返回 200。
- 第二次发布实测 Nginx reload 到两个域名均返回 200 用时 149ms。
- 旧容器仍在 `3000` 端口，作为回滚实例保留。
