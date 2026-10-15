# ossync

[English](README.md) | [简体中文](README_CN.md)

`ossync` 将本地目录单向同步到阿里云 OSS：本地新增或修改的文件会上传，OSS 中本地已不存在的对象会删除。执行前会列出完整任务并要求确认。

## 配置

默认读取待同步目录中的 `.ossyncconfig`：

```yaml
access_key_id: xxx
access_key_secret: xxx
prefix: oss://bucket/path
region: cn-shanghai
```

默认读取待同步目录中的 `.ossyncignore`，语法与 `.gitignore` 一致。被忽略的本地文件不会上传，对应的远端对象也不会被删除。配置文件和 ignore 文件自身始终被排除。

## 使用

```bash
# 当前目录，先打印任务，再询问是否执行
ossync

# 指定目录
ossync --dir ./public

# 指定配置与 ignore 文件
ossync --dir ./public --config ./deploy.yml --ignore ./deploy.ignore

# 只查看 Create / Modify / Remove 任务
ossync --dir ./public --dry

# 展示全部变化文件（默认最多展示 12 条）
ossync --dir ./public --all

# 设置删除和上传请求的最大并发数（默认 8）
ossync --dir ./public --concurrency 16

# 强制将所有本地文件作为 Create 上传
ossync --dir ./public --force

# 跳过确认（适合 CI）
ossync --dir ./public --yes
```
