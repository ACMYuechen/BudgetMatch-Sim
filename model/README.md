# model/ — 数据模型层

数据模型层，使用 goctl 配合自定义 GORM 模板生成。

## 骨架演进

- 紧急/复杂/demo业务不适合立即拆分时, 先落地最外层 `model/`，**多个服务共享**。这是骨架阶段的简化设计，降低初期复杂度。
- 业务边界稳定后，model 会下沉到数据归属的服务内部：

示例:
（共享）:                         （下沉）:
model/user/                       services/rpc/auth/model/user/
model/user_profile/               services/rpc/auth/model/user_profile/

## 生成 Model

```bash
export DB_URL="postgres://user:password@host:port/dbname?sslmode=disable"

# 用法: ./model.sh <输出目录> <表名>
./model/model.sh model/user users
```

## 目录结构

```
model/
├── <table>/                    # 用表名单数形式命名
│   ├── <table>_model.go        # 自定义接口（可安全编辑）
│   ├── <table>_model_gen.go    # 生成的基础 CRUD（可安全编辑）
│   └── vars.go                 # 常量定义（角色、状态枚举）
```

示例：
```
model/
├── user/
│   ├── users_model.go
│   ├── users_model_gen.go
│   ├── users_cache.go          # 可选：需要缓存时自行封装
│   └── vars.go
└── user_profile/
    ├── user_profiles_model.go
    ├── user_profiles_model_gen.go
    └── vars.go
```

## 自定义模板原理

`tpls/model/model.tpl` 将生成的代码包装在 `custom*Model` 结构中：

```go
// 生成的代码
type customUsersModel struct {
    *defaultUsersModel   // 嵌入基础实现
}

// 你可以在 *_model.go 中添加自定义方法
// 而不需要修改 *_model_gen.go
```

## 规范

1. **目录命名**: 用表名的**单数**形式（如 `users` 表 → `model/user/`）
2. **自动建表**: `svc.NewServiceContext()` 中调用 `CreateTable()`
3. **字段注释**: 在数据库层写好 comment，生成时会自动同步
