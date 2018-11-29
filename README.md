# ossync

一个用于同步本地文件到阿里云OSS的工具, 可用于部署静态页面.

![](demo.gif)

## 示例

### OSS配置

在需要同步的目录下放置一个名为 `.ossyncconfig` 的json文件, 内容如下.
```json
{
    "accessKeyId": "xxx",
    "accessKeySecret": "xxx",
    "bucket": "test",
    "region": "oss-cn-shanghai"
}
```

### 开始同步

```bash
$ cd local_src_dir # 进入需要同步的目录
$ ossync sync
```

### 忽略目录或文件 (不是必需的)

在需要同步的目录下放置一个名为 `.ossyncignore` 的文件, 其规则同 `.gitignore`, 命中规则的文件将被排除. 
例如:
```
.idea
.vscode
.DS_Store
.git*
*~
```

## 打包为单个可执行文件

```bash
$ git clone https://github.com/xiaozhuai/ossync
$ cd ossync
$ yarn # 安装依赖
$ yarn run build-macos64 # 可选平台有: macos64, linux64, win32, win64
```

## 局限性

1. ossync适合做为一个部署工具, 如部署一个托管在oss的静态网站. 它不应该作为大量文件传输的方式. 
2. ossync的数据是单向的, 即从本地文件系统到OSS.
3. ossync的同步策略默认是增量模式, 它的增量实现依赖于文件的特征值, 尽管ossync采用了效率极高的xxhash, 
但这仍会带来一定的性能损失. 这也是为什么它不适合作为大量文件传输的方式. 通常文件数在10000个以内比较适用.

## Releases

请在release页下载打包好的二进制版本 `binaries.zip`
