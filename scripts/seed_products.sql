-- 商品 + SKU 种子数据
-- 用途：为 Issue #46 订单状态修复提供可测试的商品数据
-- 使用方法：
--   docker exec -i budgetmatch-sim-postgres psql -U root -d budgetmatch-sim < scripts/seed_products.sql
-- 或在 WSL 中：
--   psql -h localhost -U root -d budgetmatch-sim < scripts/seed_products.sql

-- ==========================================
-- 1. 插入商品（SPU）
-- ==========================================
INSERT INTO products (id, user_id, name, content, image, providor, status, agent_comment, created_at, updated_at)
VALUES
(
  'spu-1001',
  'system-admin',
  '预算友好型机械键盘',
  '{"detail": "87键机械键盘，青轴，RGB背光，铝合金面板", "images": []}',
  'https://trae-api-cn.mchost.guru/api/ide/v1/text_to_image?prompt=mechanical%20keyboard%20blue%20switch%20RGB%20backlit%20product%20photo&image_size=landscape_4_3',
  'KeyTech',
  1,
  '{"summary": "性价比高的入门机械键盘，适合办公和游戏"}',
  NOW(),
  NOW()
),
(
  'spu-1002',
  'system-admin',
  '无线蓝牙鼠标',
  '{"detail": "静音点击，2.4G+蓝牙双模，Type-C充电", "images": []}',
  'https://trae-api-cn.mchost.guru/api/ide/v1/text_to_image?prompt=wireless%20bluetooth%20mouse%20silent%20click%20white%20product%20photo&image_size=landscape_4_3',
  'MousePro',
  1,
  '{"summary": "静音双模鼠标，办公场景首选"}',
  NOW(),
  NOW()
),
(
  'spu-1003',
  'system-admin',
  '27英寸4K显示器',
  '{"detail": "IPS面板，HDR400，Type-C 65W反向充电，升降支架", "images": []}',
  'https://trae-api-cn.mchost.guru/api/ide/v1/text_to_image?prompt=27%20inch%204K%20monitor%20IPS%20HDR400%20product%20photo&image_size=landscape_4_3',
  'ViewMax',
  1,
  '{"summary": "4K IPS显示器，色彩还原度高，适合设计和开发"}',
  NOW(),
  NOW()
),
(
  'spu-1004',
  'system-admin',
  'USB-C 扩展坞',
  '{"detail": "7合1扩展坞，HDMI 4K@60Hz，PD 100W充电，千兆网口", "images": []}',
  'https://trae-api-cn.mchost.guru/api/ide/v1/text_to_image?prompt=USB-C%20hub%20docking%20station%207-in-1%20product%20photo&image_size=landscape_4_3',
  'DockTech',
  1,
  '{"summary": "功能齐全的扩展坞，覆盖主流接口需求"}',
  NOW(),
  NOW()
),
(
  'spu-1005',
  'system-admin',
  '人体工学办公椅',
  '{"detail": "腰部支撑，可调节扶手，透气网布，3D头枕", "images": []}',
  'https://trae-api-cn.mchost.guru/api/ide/v1/text_to_image?prompt=ergonomic%20office%20chair%20mesh%20back%20lumbar%20support%20product%20photo&image_size=landscape_4_3',
  'ErgoChair',
  1,
  '{"summary": "久坐不累，适合长时间办公"}',
  NOW(),
  NOW()
),
(
  'spu-1006',
  'system-admin',
  '降噪蓝牙耳机',
  '{"detail": "主动降噪，蓝牙5.3，续航30小时，快充10分钟用5小时", "images": []}',
  'https://trae-api-cn.mchost.guru/api/ide/v1/text_to_image?prompt=noise%20canceling%20bluetooth%20headphones%20ANC%20black%20product%20photo&image_size=landscape_4_3',
  'AudioMax',
  1,
  '{"summary": "降噪效果优秀，续航持久，通勤办公两用"}',
  NOW(),
  NOW()
)
ON CONFLICT (id) DO NOTHING;

-- ==========================================
-- 2. 插入 SKU（每个商品 2-3 个规格）
-- ==========================================
INSERT INTO product_skus (id, product_id, name, specs, price, stock, sold, status, agent_comment, created_at, updated_at)
VALUES
-- 机械键盘：3个轴体
('sku-1001-1', 'spu-1001', '青轴版', '{"switch": "blue", "layout": "87键"}',  19900, 50, 12, 1, '{"summary": "青轴手感清脆，适合打字"}', NOW(), NOW()),
('sku-1001-2', 'spu-1001', '红轴版', '{"switch": "red", "layout": "87键"}',   19900, 30, 8,  1, '{"summary": "红轴安静顺滑，适合办公"}', NOW(), NOW()),
('sku-1001-3', 'spu-1001', '茶轴版', '{"switch": "brown", "layout": "87键"}',  20900, 20, 5,  1, '{"summary": "茶轴兼顾手感和静音"}', NOW(), NOW()),

-- 蓝牙鼠标：2个颜色
('sku-1002-1', 'spu-1002', '白色版', '{"color": "white"}',  8900, 100, 25, 1, '{"summary": "白色简约风格"}', NOW(), NOW()),
('sku-1002-2', 'spu-1002', '黑色版', '{"color": "black"}',  8900, 80,  18, 1, '{"summary": "黑色商务风格"}', NOW(), NOW()),

-- 4K显示器：2个配置
('sku-1003-1', 'spu-1003', '标准支架版', '{"stand": "standard"}',  189900, 15, 3, 1, '{"summary": "标准升降支架"}', NOW(), NOW()),
('sku-1003-2', 'spu-1003', '旋转支架版', '{"stand": "rotating"}',  199900, 10, 2, 1, '{"summary": "支持竖屏旋转"}', NOW(), NOW()),

-- 扩展坞
('sku-1004-1', 'spu-1004', '7合1标准版', '{"ports": "7"}',  14900, 60, 10, 1, '{"summary": "7合1主流接口"}', NOW(), NOW()),

-- 办公椅
('sku-1005-1', 'spu-1005', '黑色网布版', '{"color": "black", "material": "mesh"}',  59900, 25, 6, 1, '{"summary": "透气网布，夏天不闷"}', NOW(), NOW()),
('sku-1005-2', 'spu-1005', '灰色网布版', '{"color": "gray", "material": "mesh"}',   59900, 20, 4, 1, '{"summary": "灰色低调百搭"}', NOW(), NOW()),

-- 耳机
('sku-1006-1', 'spu-1006', '头戴式黑色', '{"type": "over-ear", "color": "black"}',  49900, 40, 15, 1, '{"summary": "头戴式降噪，佩戴舒适"}', NOW(), NOW()),
('sku-1006-2', 'spu-1006', '入耳式白色', '{"type": "in-ear", "color": "white"}',    39900, 50, 20, 1, '{"summary": "入耳式轻便，携带方便"}', NOW(), NOW())

ON CONFLICT (id) DO NOTHING;

-- ==========================================
-- 3. 验证数据
-- ==========================================
SELECT '--- 商品数量 ---' AS info;
SELECT COUNT(*) AS product_count FROM products WHERE status = 1;

SELECT '--- SKU数量 ---' AS info;
SELECT COUNT(*) AS sku_count FROM product_skus WHERE status = 1;

SELECT '--- 商品+SKU概览 ---' AS info;
SELECT
  p.id   AS spu_id,
  p.name AS spu_name,
  p.status,
  s.id   AS sku_id,
  s.name AS sku_name,
  s.price,
  s.stock
FROM products p
LEFT JOIN product_skus s ON s.product_id = p.id
WHERE p.status = 1
ORDER BY p.id, s.id;
