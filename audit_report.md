# transphorm-quiz-app 代码审查与并发安全报告

作者：Manus AI

本文档对 `transphorm-quiz-app` 项目的后端（Go + GORM + SQLite）及前端页面（HTML/JS）进行了全面深度审查，重点分析了在高并发场景（如 1000 人同时在线）下可能出现的性能瓶颈、数据冲突、并发竞争风险以及前端安全隐患。

## 一、 并发安全与数据冲突风险

项目采用 SQLite 作为底层数据库。SQLite 默认使用文件级锁，在写密集型的高并发场景下容易产生 `database is locked` 错误，从而导致请求失败。此外，业务逻辑中的部分实现存在明显的并发漏洞。

### 1. 扫码增加活动积分（并发重复扫码风险）

在 `backend/handlers/shop.go` 的 `ScanActivity` 接口中，业务逻辑为：
1. 验证二维码签名和有效期（5分钟）。
2. 检查活动是否有效。
3. 直接调用 `config.DB.Create(&record)` 写入 `Redemption` 记录。

**问题分析**：
该接口没有对同一个二维码（或同一用户对同一活动）进行幂等性校验或防重放攻击保护。如果一个用户截屏二维码并由多台设备同时扫码，或者利用脚本并发发送请求，系统将为该用户重复创建多条加分记录，导致用户积分被恶意刷高。
此外，二维码的 payload 为 `userID|employeeID|name|timestamp`，其中缺少唯一随机数（Nonce/UUID）。只要在 5 分钟有效期内，任何人拿到这个二维码都可以无限次请求增加积分。

### 2. 商品兑换与库存扣减（读写分离与并发竞争）

在 `backend/handlers/shop.go` 的 `RedeemProduct` 接口中，虽然使用了数据库事务，但其实现方式存在漏洞：
```go
// 事务内重新读取商品
var p models.Product
if err := tx.First(&p, req.ProductID).Error; err != nil { ... }
if p.Stock <= 0 { ... }

// 扣减库存
if err := tx.Model(&p).UpdateColumn("stock", gorm.Expr("stock - 1")).Error; err != nil { ... }
```

**问题分析**：
在事务中，`tx.First(&p, req.ProductID)` 是一个普通的 `SELECT` 语句，并没有使用排他锁（如 `FOR UPDATE`）。在 1000 人并发抢兑同一件热门商品时，多个事务可能同时读取到 `Stock = 1`。随后，这些事务都会通过 `stock - 1` 成功扣减库存，导致库存出现负数（超卖现象）。
正确的做法是在更新时将库存大于 0 作为更新条件，或者在查询时加锁。

### 3. 积分计算的性能瓶颈

用户的可用积分并非保存在 `User` 表的单个字段中，而是在每次需要展示或使用积分时，通过 `getUserPointsBreakdown` 动态聚合计算：
```go
var actSum struct{ Total int }
config.DB.Model(&models.Redemption{}).Select("COALESCE(SUM(points),0) as total").
    Where("user_id=? AND type='activity' AND status='success'", userID).Scan(&actSum)
```

**问题分析**：
每次兑换商品、查看积分详情时，系统都需要对 `Redemption` 表进行多次 `SUM` 聚合查询。随着活动进行，`Redemption` 表的数据量会迅速膨胀。在高并发下，这种实时聚合查询会严重拖慢 SQLite 的读取性能，甚至引发读写锁冲突。

## 二、 前端安全与性能隐患

前端页面采用了原生 HTML、CSS 和 JavaScript 编写，主要通过 `fetch` 调用后端 API 并使用模板字符串渲染 DOM。

### 1. XSS（跨站脚本攻击）漏洞

在 `backend/public/admin/index.html` 和 `backend/public/shop/index.html` 中，大量使用了 `innerHTML` 进行数据绑定。例如：
```javascript
tbody.innerHTML = sorted.map((r, i) => `
  <tr>
    <td>${r.employee_id}</td>
    <td>${r.name}</td>
    ...
  </tr>
`).join('');
```

**问题分析**：
如果用户的 `name` 或 `office` 字段包含恶意 HTML/JavaScript 代码（例如 `<script>alert(1)</script>`），当管理员或商家在后台查看列表时，这些代码将被浏览器直接执行。这不仅会导致页面结构破坏，还可能被用来窃取管理员的 `localStorage` Token，从而接管整个系统。

### 2. 接口分页滥用与前端性能崩溃

在 `backend/public/admin/index.html` 中，前端为了在客户端计算全局统计数据，直接发起超大 `page_size` 的请求：
```javascript
const res = await fetch(`${API_BASE}/admin/scores?page=1&page_size=2000`, ...);
const rRes = await fetch(`${API_BASE}/admin/redemptions?page_size=10000`, ...);
```

**问题分析**：
这种设计极其危险。对于一个 1000 人的活动，系统将一次性从数据库中拉取并序列化上万条记录，通过网络传输到前端，再由前端进行遍历和 DOM 渲染。这会导致：
1. 后端 SQLite 数据库因大查询被长时间占用。
2. 服务器内存和网络带宽被瞬间打满。
3. 浏览器端因处理庞大的 JSON 和生成巨大的 DOM 树而卡顿甚至崩溃。

## 三、 其他功能性 Bug 与设计缺陷

### 1. 配置更新接口缺乏验证

在 `backend/handlers/config.go` 的 `UpdateConfig` 接口中，系统接收任意的键值对并直接更新或插入到数据库中：
```go
for k, v := range req {
    var cfg models.Config
    result := config.DB.Where("key = ?", k).First(&cfg)
    if result.Error != nil {
        config.DB.Create(&models.Config{Key: k, Value: v})
    } else {
        config.DB.Model(&cfg).Update("value", v)
    }
}
```

**问题分析**：
该接口没有对 `key` 进行白名单校验。恶意管理员或被盗用的账号可以注入任意配置项，或者覆盖系统的关键配置（如将 `background_image` 修改为恶意链接），甚至填入超大文本导致数据库膨胀。

### 2. 文件上传路径安全风险

在 `UploadBackground` 和 `UploadProductImage` 接口中，虽然限制了文件扩展名，但使用了固定的静态目录：
```go
savePath := staticDir + "/bg" + ext
```

**问题分析**：
如果用户上传的扩展名带有特殊字符，或者系统未来扩展了其他类型的文件上传，直接拼接路径可能带来目录遍历风险。此外，背景图上传直接覆盖了 `bg.ext`，如果并发上传，会导致文件被意外覆盖或损坏。

### 3. 退回活动积分的状态更新不一致

在 `backend/handlers/shop.go` 的 `RefundActivity` 中：
```go
if err := config.DB.Model(&record).Updates(map[string]interface{}{
    "status": "refunded",
    "remark": fmt.Sprintf("活动积分已退回（操作人ID：%d）", operatorID),
}).Error; err != nil { ... }
```

**问题分析**：
该操作没有放在事务中执行。如果状态更新成功，但后续的积分计算或响应失败，可能会导致数据状态不一致。虽然积分是动态计算的，但这种非事务性的关键状态修改在严谨的系统中是不推荐的。

## 四、 总结与修复建议

`transphorm-quiz-app` 项目在功能实现上较为完整，但在高并发场景和安全性设计上存在明显短板。为了支撑 1000 人以上的同时使用，建议进行以下改造：

1. **数据库与并发控制**：将底层数据库从 SQLite 迁移至 MySQL 或 PostgreSQL，以支持行级锁和更高的并发写入能力。在扣减库存时，必须在 SQL 语句中加入 `stock > 0` 的条件（乐观锁），避免超卖。
2. **积分与幂等性**：为二维码增加全局唯一的 Nonce，并在数据库中记录已使用的 Nonce，防止重放攻击。考虑在 `User` 表中冗余存储积分字段，通过事务同步更新，避免每次都进行全表 `SUM` 聚合。
3. **前端安全**：将所有的 `innerHTML` 替换为安全的 DOM 操作（如 `textContent` 或 `innerText`），或者引入 DOMPurify 等库对渲染内容进行 HTML 实体转义。
4. **接口优化**：取消前端的大分页请求，将统计聚合逻辑（如总人数、总兑换量）移至后端，通过专门的统计 API 返回结果。

通过上述修复，系统将能够稳定、安全地应对高并发挑战。
