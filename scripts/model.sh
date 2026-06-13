# Model 生成脚本
# 依赖: goctl 已安装，DB_URL 环境变量已设置
#
# 用法:
#   export DB_URL="postgres://user:password@host:port/dbname?sslmode=disable"
#   ./model.sh <相对项目根目录的输出目录> <表名>
#
# 示例:
#   ./model.sh model/user users

goctl model pg datasource \
  -url "$DB_URL" \
  --dir "$1" \
  --table "$2" \
  --home ./tpls \

# 生成的 model 已适配 GORM 风格，只需将sql.*类型替换即可
