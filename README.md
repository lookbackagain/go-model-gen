# sgt

sino 代码生成工具。

默认从 init.yaml 文件读取配置。

支持功能：

- 从配置结构体生成 models 层代码
- 从配置文件定义的 MySQL 连接信息生成 models 层代码

使用示例:

在 init.yaml 文件定义结构体：

```yaml
models:
- name: UserMeta
  fields:
  - name: UserName
    type: string
    comment: 这是注释
  - name: Photo
    type: '*string'
    tag: 'tag1:"test_tag"'
  - name: Age
    type: int32
- name: Funs
  fields:
  - name: Uid
    type: int32
  - name: Status
    type: '*string'
  - name: StartTs
    type: int32
db_config:
- name: "activitys"
  database: train_live
  username: root
  password: "123456"
  host: 10.0.3.190
  port: 3306
  table: "tl_activitys"
  alias_name: "activitys"
```

输入生成 models 代码命令：

```
sgt model --yaml init.yaml
或直接
sgt model
```
