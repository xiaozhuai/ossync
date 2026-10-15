# ossync

[English](README.md) | [简体中文](README_CN.md)

`ossync` performs one-way synchronization from a local directory to Alibaba Cloud OSS. New and modified local files are uploaded, while OSS objects that no longer exist locally are removed. Before making changes, it displays the planned tasks and asks for confirmation.

## Configuration

By default, `ossync` reads `.ossyncconfig` from the directory being synchronized:

```yaml
access_key_id: xxx
access_key_secret: xxx
prefix: oss://bucket/path
region: cn-shanghai
```

By default, `ossync` also reads `.ossyncignore` from the directory being synchronized. Its syntax is compatible with `.gitignore`. Ignored local files are not uploaded, and their corresponding remote objects are not removed. The configuration and ignore files themselves are always excluded.

## Usage

```bash
# Synchronize the current directory: show tasks first, then ask for confirmation
ossync

# Specify a directory
ossync --dir ./public

# Specify configuration and ignore files
ossync --dir ./public --config ./deploy.yml --ignore ./deploy.ignore

# Show Create / Modify / Remove tasks without applying them
ossync --dir ./public --dry

# Show all changed files (at most 12 are shown by default)
ossync --dir ./public --all

# Set the maximum number of concurrent delete and upload requests (default: 8)
ossync --dir ./public --concurrency 16

# Force every local file to be uploaded as Create
ossync --dir ./public --force

# Skip confirmation (useful in CI)
ossync --dir ./public --yes
```
