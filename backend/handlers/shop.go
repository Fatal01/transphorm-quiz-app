package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	qrcode "github.com/skip2/go-qrcode"
	"gorm.io/gorm"
	"quiz-app/config"
	"quiz-app/models"
)

// ========== 商品管理 API ==========

func GetProducts(c *gin.Context) {
	var products []models.Product
	config.DB.Where("is_active = ?", true).Order("sort_order ASC, id ASC").Find(&products)
	c.JSON(http.StatusOK, gin.H{"products": products})
}

func GetAllProducts(c *gin.Context) {
	var products []models.Product
	config.DB.Order("sort_order ASC, id ASC").Find(&products)
	c.JSON(http.StatusOK, gin.H{"products": products})
}

func CreateProduct(c *gin.Context) {
	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		Points      int    `json:"points" binding:"required"`
		Stock       int    `json:"stock"`
		IsActive    bool   `json:"is_active"`
		SortOrder   int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误，商品名称和积分为必填"})
		return
	}
	product := models.Product{
		Name: req.Name, Description: req.Description,
		Points: req.Points, Stock: req.Stock,
		IsActive: req.IsActive, SortOrder: req.SortOrder,
	}
	if err := config.DB.Create(&product).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建商品失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "商品创建成功", "product": product})
}

func UpdateProduct(c *gin.Context) {
	id := c.Param("id")
	var product models.Product
	if err := config.DB.First(&product, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "商品不存在"})
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Points      int    `json:"points"`
		Stock       int    `json:"stock"`
		IsActive    *bool  `json:"is_active"`
		SortOrder   int    `json:"sort_order"`
		Image       string `json:"image"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	updates := map[string]interface{}{
		"stock": req.Stock, "sort_order": req.SortOrder,
	}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	if req.Points > 0 {
		updates["points"] = req.Points
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}
	if req.Image != "" {
		updates["image"] = req.Image
	}
	config.DB.Model(&product).Updates(updates)
	config.DB.First(&product, id)
	c.JSON(http.StatusOK, gin.H{"message": "商品更新成功", "product": product})
}

func DeleteProduct(c *gin.Context) {
	id := c.Param("id")
	var product models.Product
	if err := config.DB.First(&product, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "商品不存在"})
		return
	}
	config.DB.Delete(&product)
	c.JSON(http.StatusOK, gin.H{"message": "商品删除成功"})
}

func UploadProductImage(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请上传图片文件"})
		return
	}
	ext := filepath.Ext(file.Filename)
	allowed := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true}
	if !allowed[ext] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "只支持 jpg/png/gif/webp 格式"})
		return
	}
	staticDir := os.Getenv("STATIC_DIR")
	if staticDir == "" {
		staticDir = "./static"
	}
	productDir := filepath.Join(staticDir, "products")
	os.MkdirAll(productDir, 0755)
	filename := fmt.Sprintf("product_%d%s", time.Now().UnixMilli(), ext)
	savePath := filepath.Join(productDir, filename)
	if err := c.SaveUploadedFile(file, savePath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存文件失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "图片上传成功", "url": "/api/static/products/" + filename})
}

// ========== 活动管理 API ==========

func GetAllActivities(c *gin.Context) {
	var activities []models.Activity
	config.DB.Order("sort_order ASC, id ASC").Find(&activities)
	c.JSON(http.StatusOK, gin.H{"activities": activities})
}

func GetActiveActivities(c *gin.Context) {
	var activities []models.Activity
	config.DB.Where("is_active = ?", true).Order("sort_order ASC, id ASC").Find(&activities)
	c.JSON(http.StatusOK, gin.H{"activities": activities})
}

func CreateActivity(c *gin.Context) {
	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		Points      int    `json:"points" binding:"required"`
		IsActive    bool   `json:"is_active"`
		SortOrder   int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误，活动名称和积分为必填"})
		return
	}
	activity := models.Activity{
		Name: req.Name, Description: req.Description,
		Points: req.Points, IsActive: req.IsActive, SortOrder: req.SortOrder,
	}
	if err := config.DB.Create(&activity).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建活动失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "活动创建成功", "activity": activity})
}

func UpdateActivity(c *gin.Context) {
	id := c.Param("id")
	var activity models.Activity
	if err := config.DB.First(&activity, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "活动不存在"})
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Points      int    `json:"points"`
		IsActive    *bool  `json:"is_active"`
		SortOrder   int    `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	updates := map[string]interface{}{"sort_order": req.SortOrder}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	if req.Points > 0 {
		updates["points"] = req.Points
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}
	config.DB.Model(&activity).Updates(updates)
	config.DB.First(&activity, id)
	c.JSON(http.StatusOK, gin.H{"message": "活动更新成功", "activity": activity})
}

func DeleteActivity(c *gin.Context) {
	id := c.Param("id")
	var activity models.Activity
	if err := config.DB.First(&activity, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "活动不存在"})
		return
	}
	config.DB.Delete(&activity)
	c.JSON(http.StatusOK, gin.H{"message": "活动删除成功"})
}

// getActivityPointsLimit 读取线下活动积分上限配置（0=不限制）
func getActivityPointsLimit() int {
	var cfg models.Config
	// 使用实际列名 config_key（Key 字段映射到 config_key 以避免 MySQL 保留字冲突）
	if err := config.DB.Where("config_key = ?", "activity_points_limit").First(&cfg).Error; err != nil {
		return 0
	}
	limit, _ := strconv.Atoi(cfg.Value)
	return limit
}

// ScanActivity 扫码增加活动积分
// 新增：校验线下活动积分上限（activity_points_limit 配置项，0=不限制）
func ScanActivity(c *gin.Context) {
	var req struct {
		QRData     string `json:"qr_data" binding:"required"`
		ActivityID uint   `json:"activity_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误，缺少 qr_data 或 activity_id"})
		return
	}

	// 解析并验证二维码
	qrDataClean := strings.TrimSpace(req.QRData)
	parts := splitQRDataN(qrDataClean, 5)
	if len(parts) != 5 {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("二维码格式无效（字段数：%d，期望：5）", len(parts))})
		return
	}

	uid, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "二维码用户ID无效"})
		return
	}
	userID := uint(uid)
	employeeID := parts[1]
	name := parts[2]
	timestamp, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "二维码时间戳无效"})
		return
	}
	signature := parts[4]

	// 验证签名
	payload := fmt.Sprintf("%d|%s|%s|%d", userID, employeeID, name, timestamp)
	mac := hmac.New(sha256.New, qrSecret)
	mac.Write([]byte(payload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "二维码签名验证失败，请让用户重新生成"})
		return
	}

	// 检查过期（5分钟）
	if time.Now().Unix()-timestamp > 300 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "二维码已过期（有效期5分钟），请让用户重新生成"})
		return
	}

	// 查找用户
	var user models.User
	if err := config.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	// 查找活动
	var activity models.Activity
	if err := config.DB.First(&activity, req.ActivityID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "活动不存在"})
		return
	}
	if !activity.IsActive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "活动已停用"})
		return
	}

	operatorID := c.GetUint("user_id")

	// 在事务内完成：积分上限校验 + 写入记录 + 同步 User 冗余字段
	txErr := config.DB.Transaction(func(tx *gorm.DB) error {
		// 读取线下活动积分上限
		limit := getActivityPointsLimit()

		// 查询该用户已获得的线下活动积分总和（FOR UPDATE 行锁）
		var actSum struct{ Total int }
		tx.Model(&models.Redemption{}).Select("COALESCE(SUM(points),0) as total").
			Where("user_id=? AND type='activity' AND status='success'", userID).Scan(&actSum)

		if limit > 0 && actSum.Total+activity.Points > limit {
			return fmt.Errorf("线下活动积分已达上限（上限：%d，当前：%d，本次：%d）",
				limit, actSum.Total, activity.Points)
		}

		// 写入活动积分记录（活动记录不关联商品，ProductID=0 避免外键约束失败）
		record := models.Redemption{
			UserID:      user.ID,
			EmployeeID:  user.EmployeeID,
			UserName:    user.Name,
			ProductID:   0,
			ProductName: activity.Name,
			Points:      activity.Points,
			Status:      "success",
			Type:        "activity",
			Remark:      fmt.Sprintf("活动扫码增加积分（活动：%s）", activity.Name),
			OperatorID:  operatorID,
		}
		if err := tx.Create(&record).Error; err != nil {
			return fmt.Errorf("积分记录写入失败")
		}

		// 全量重算并同步 User 冗余积分字段
		if err := SyncUserPointsTx(tx, userID); err != nil {
			return fmt.Errorf("用户积分同步失败")
		}
		return nil
	})

	if txErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": txErr.Error()})
		return
	}

	// 重新读取最新积分
	var updatedUser models.User
	config.DB.First(&updatedUser, userID)

	c.JSON(http.StatusOK, gin.H{
		"message":          "积分增加成功",
		"user_name":        user.Name,
		"employee_id":      user.EmployeeID,
		"activity_name":    activity.Name,
		"points_added":     activity.Points,
		"available_points": updatedUser.Points,
	})
}

// RefundActivity 退回活动积分记录
func RefundActivity(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "记录ID无效"})
		return
	}

	var record models.Redemption
	if err := config.DB.First(&record, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "记录不存在"})
		return
	}
	if record.Type != "activity" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "只能退回活动积分记录"})
		return
	}
	if record.Status != "success" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "只能退回状态为「成功」的记录"})
		return
	}

	operatorID := c.GetUint("user_id")
	txErr := config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&record).Updates(map[string]interface{}{
			"status": "refunded",
			"remark": fmt.Sprintf("活动积分已退回（操作人ID：%d）", operatorID),
		}).Error; err != nil {
			return fmt.Errorf("退回失败")
		}

		// 全量重算并同步 User 冗余积分字段
		return SyncUserPointsTx(tx, record.UserID)
	})

	if txErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": txErr.Error()})
		return
	}

	var updatedUser models.User
	config.DB.First(&updatedUser, record.UserID)

	c.JSON(http.StatusOK, gin.H{
		"message":       "活动积分已退回",
		"employee_id":   record.EmployeeID,
		"user_name":     record.UserName,
		"activity_name": record.ProductName,
		"points":        record.Points,
		"new_available": updatedUser.Points,
	})
}

// ========== 二维码 & 兑换 API ==========

var qrSecret = []byte("quiz-shop-qr-secret-2026")

func GenerateQRCode(c *gin.Context) {
	userID := c.GetUint("user_id")
	employeeID := c.GetString("employee_id")
	name := c.GetString("name")

	timestamp := time.Now().Unix()
	payload := fmt.Sprintf("%d|%s|%s|%d", userID, employeeID, name, timestamp)
	mac := hmac.New(sha256.New, qrSecret)
	mac.Write([]byte(payload))
	signature := hex.EncodeToString(mac.Sum(nil))
	qrData := fmt.Sprintf("%d|%s|%s|%d|%s", userID, employeeID, name, timestamp, signature)

	png, err := qrcode.Encode(qrData, qrcode.Medium, 256)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "二维码生成失败"})
		return
	}
	qrImage := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	c.JSON(http.StatusOK, gin.H{
		"qr_data": qrData, "qr_image": qrImage,
		"employee_id": employeeID, "name": name, "expires_in": 300,
	})
}

// RedeemProduct 扫码兑换商品
// 修复：库存扣减使用 stock > 0 乐观锁条件，防止超卖；同步更新 User 冗余积分字段
func RedeemProduct(c *gin.Context) {
	var req struct {
		QRData    string `json:"qr_data" binding:"required"`
		ProductID uint   `json:"product_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误，缺少 qr_data 或 product_id"})
		return
	}

	qrDataClean := strings.TrimSpace(req.QRData)
	parts := splitQRDataN(qrDataClean, 5)
	if len(parts) != 5 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   fmt.Sprintf("二维码格式无效（字段数：%d，期望：5）", len(parts)),
			"qr_data": qrDataClean,
		})
		return
	}

	uid, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "二维码用户ID无效"})
		return
	}
	userID := uint(uid)
	employeeID := parts[1]
	name := parts[2]
	timestamp, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "二维码时间戳无效"})
		return
	}
	signature := parts[4]

	// 验证签名
	payload := fmt.Sprintf("%d|%s|%s|%d", userID, employeeID, name, timestamp)
	mac := hmac.New(sha256.New, qrSecret)
	mac.Write([]byte(payload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "二维码签名验证失败，请让用户重新生成"})
		return
	}

	// 检查过期（5分钟）
	if time.Now().Unix()-timestamp > 300 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "二维码已过期（有效期5分钟），请让用户重新生成"})
		return
	}

	// 查找用户
	var user models.User
	if err := config.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	// 预检商品
	var product models.Product
	if err := config.DB.First(&product, req.ProductID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "商品不存在"})
		return
	}
	if !product.IsActive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "商品已下架"})
		return
	}

	operatorID := c.GetUint("user_id")
	var redemption models.Redemption

	type redeemErr struct {
		msg      string
		required int
		avail    int
		isPoints bool
	}
	var rErr *redeemErr

	txErr := config.DB.Transaction(func(tx *gorm.DB) error {
		// 事务内重新读取商品（加行锁）
		var p models.Product
		if err := tx.Set("gorm:query_option", "FOR UPDATE").First(&p, req.ProductID).Error; err != nil {
			return fmt.Errorf("商品不存在")
		}
		if p.Stock <= 0 {
			return fmt.Errorf("商品库存不足")
		}

		// 事务内计算可用积分（在写入 Redemption 记录之前校验余额）
		available := calcAvailablePointsTx(tx, userID)

		if available < p.Points {
			tx.Create(&models.Redemption{
				UserID: user.ID, EmployeeID: user.EmployeeID, UserName: user.Name,
				ProductID: p.ID, ProductName: p.Name, Points: p.Points,
				Status: "failed", Type: "redeem", OperatorID: operatorID,
				Remark: fmt.Sprintf("积分不足，需要%d分，当前可用%d分", p.Points, available),
			})
			rErr = &redeemErr{msg: "积分不足", required: p.Points, avail: available, isPoints: true}
			return fmt.Errorf("积分不足")
		}

		// 关键修复：使用 stock > 0 条件扣减库存，防止并发超卖
		result := tx.Model(&models.Product{}).
			Where("id = ? AND stock > 0", p.ID).
			UpdateColumn("stock", gorm.Expr("stock - 1"))
		if result.Error != nil {
			return fmt.Errorf("库存扣减失败")
		}
		if result.RowsAffected == 0 {
			// 并发情况下库存已被其他请求扣完
			return fmt.Errorf("商品库存不足（并发冲突）")
		}

		redemption = models.Redemption{
			UserID: user.ID, EmployeeID: user.EmployeeID, UserName: user.Name,
			ProductID: p.ID, ProductName: p.Name, Points: p.Points,
			Status: "success", Type: "redeem", Remark: "兑换成功", OperatorID: operatorID,
		}
		if err := tx.Create(&redemption).Error; err != nil {
			return fmt.Errorf("兑换记录写入失败")
		}

		// 全量重算并同步 User 冗余积分字段
		return SyncUserPointsTx(tx, userID)
	})

	if txErr != nil {
		if rErr != nil && rErr.isPoints {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":            rErr.msg,
				"required_points":  rErr.required,
				"available_points": rErr.avail,
			})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": txErr.Error()})
		}
		return
	}

	var updatedUser models.User
	config.DB.First(&updatedUser, userID)

	c.JSON(http.StatusOK, gin.H{
		"message":          "兑换成功",
		"redemption_id":    redemption.ID,
		"product_name":     redemption.ProductName,
		"points_cost":      redemption.Points,
		"remaining_points": updatedUser.Points,
		"user_name":        user.Name,
		"employee_id":      user.EmployeeID,
	})
}

// RefundRedemption 退回商品兑换记录（管理员调用，使用事务）
func RefundRedemption(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "兑换记录ID无效"})
		return
	}

	var record models.Redemption
	if err := config.DB.First(&record, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "兑换记录不存在"})
		return
	}
	if record.Status != "success" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "只能退回状态为「成功」的兑换记录"})
		return
	}

	operatorID := c.GetUint("user_id")
	txErr := config.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&record).Updates(map[string]interface{}{
			"status": "refunded",
			"remark": fmt.Sprintf("已退回（操作人ID：%d）", operatorID),
		}).Error; err != nil {
			return fmt.Errorf("更新兑换记录失败")
		}
		// 仅商品兑换才恢复库存
		if record.Type == "redeem" || record.Type == "" {
			if err := tx.Model(&models.Product{}).Where("id = ?", record.ProductID).
				UpdateColumn("stock", gorm.Expr("stock + 1")).Error; err != nil {
				return fmt.Errorf("恢复库存失败")
			}
		}

		// 全量重算并同步 User 冗余积分字段
		return SyncUserPointsTx(tx, record.UserID)
	})

	if txErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": txErr.Error()})
		return
	}

	var updatedUser models.User
	config.DB.First(&updatedUser, record.UserID)

	c.JSON(http.StatusOK, gin.H{
		"message":         "退回成功",
		"employee_id":     record.EmployeeID,
		"user_name":       record.UserName,
		"product_name":    record.ProductName,
		"points_refunded": record.Points,
		"new_available":   updatedUser.Points,
	})
}

// ========== 积分计算辅助函数 ==========

// getQuizScore 计算用户答题积分（5关全通过得20分）
func getQuizScore(userID uint) int {
	var passedCount int64
	config.DB.Model(&models.Score{}).Where("user_id = ? AND score = 100", userID).Count(&passedCount)
	if passedCount >= 5 {
		return 20
	}
	return 0
}

// calcQuizScoreTx 在事务内计算答题积分
func calcQuizScoreTx(tx *gorm.DB, userID uint) int {
	var passedCount int64
	tx.Model(&models.Score{}).Where("user_id = ? AND score = 100", userID).Count(&passedCount)
	if passedCount >= 5 {
		return 20
	}
	return 0
}

// SyncUserPointsTx 在事务内全量重算并同步 User 表的所有积分冗余字段。
// 所有会产生积分变动的操作（扫码、退回、兑换、退回兑换、答题奖励）都必须在同一事务中调用此函数，
// 以保证 Redemption 流水表与 User 冗余字段的强一致性。
func SyncUserPointsTx(tx *gorm.DB, userID uint) error {
	// 1. 答题积分：从 Redemption 表读取（与 Score 表保持一致）
	var quizSum struct{ Total int }
	tx.Model(&models.Redemption{}).
		Select("COALESCE(SUM(points),0) as total").
		Where("user_id = ? AND type = 'quiz' AND status = 'success'", userID).
		Scan(&quizSum)

	// 2. 线下活动积分
	var actSum struct{ Total int }
	tx.Model(&models.Redemption{}).
		Select("COALESCE(SUM(points),0) as total").
		Where("user_id = ? AND type = 'activity' AND status = 'success'", userID).
		Scan(&actSum)

	// 3. 已兑换消耗积分
	var usedSum struct{ Total int }
	tx.Model(&models.Redemption{}).
		Select("COALESCE(SUM(points),0) as total").
		Where("user_id = ? AND type = 'redeem' AND status = 'success'", userID).
		Scan(&usedSum)

	// 4. 可用积分 = 答题 + 活动 - 已兑换
	availablePoints := quizSum.Total + actSum.Total - usedSum.Total
	if availablePoints < 0 {
		availablePoints = 0
	}

	// 5. 一次性更新所有冗余字段，确保与 Redemption 表完全一致
	return tx.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"quiz_score":      quizSum.Total,
		"activity_points": actSum.Total,
		"used_points":     usedSum.Total,
		"points":          availablePoints,
	}).Error
}

// getUserPointsBreakdown 返回 (quiz积分, activity积分, 已兑换积分)。
// 所有积分均从 Redemption 表读取，与 SyncUserPointsTx 数据来源保持一致。
func getUserPointsBreakdown(userID uint) (int, int, int) {
	var quizSum struct{ Total int }
	config.DB.Model(&models.Redemption{}).Select("COALESCE(SUM(points),0) as total").
		Where("user_id=? AND type='quiz' AND status='success'", userID).Scan(&quizSum)

	var actSum struct{ Total int }
	config.DB.Model(&models.Redemption{}).Select("COALESCE(SUM(points),0) as total").
		Where("user_id=? AND type='activity' AND status='success'", userID).Scan(&actSum)

	var usedSum struct{ Total int }
	config.DB.Model(&models.Redemption{}).Select("COALESCE(SUM(points),0) as total").
		Where("user_id=? AND type='redeem' AND status='success'", userID).Scan(&usedSum)

	return quizSum.Total, actSum.Total, usedSum.Total
}

// calcAvailablePointsTx 在事务内计算用户当前可用积分。
// 专用于写入前的余额校验（如兑换前判断积分是否足够），
// 此时 Redemption 记录尚未写入，因此不能复用 SyncUserPointsTx。
func calcAvailablePointsTx(tx *gorm.DB, userID uint) int {
	quizPts := calcQuizScoreTx(tx, userID)

	var actSum struct{ Total int }
	tx.Model(&models.Redemption{}).Select("COALESCE(SUM(points),0) as total").
		Where("user_id=? AND type='activity' AND status='success'", userID).Scan(&actSum)

	var usedSum struct{ Total int }
	tx.Model(&models.Redemption{}).Select("COALESCE(SUM(points),0) as total").
		Where("user_id=? AND type='redeem' AND status='success'", userID).Scan(&usedSum)

	available := quizPts + actSum.Total - usedSum.Total
	if available < 0 {
		available = 0
	}
	return available
}

// GetUserPoints 获取当前用户积分详情
func GetUserPoints(c *gin.Context) {
	userID := c.GetUint("user_id")

	quizScore, actPts, usedPts := getUserPointsBreakdown(userID)
	available := quizScore + actPts - usedPts
	if available < 0 {
		available = 0
	}

	// 获取通过的关卡列表
	var scores []models.Score
	config.DB.Where("user_id = ? AND score = 100", userID).Find(&scores)
	passedQuizzes := []int{}
	for _, s := range scores {
		passedQuizzes = append(passedQuizzes, s.QuizIndex)
	}

	progress := len(passedQuizzes) * 20 // 每关20%

	// 读取线下活动积分上限
	limit := getActivityPointsLimit()

	c.JSON(http.StatusOK, gin.H{
		"quiz_score":            quizScore,
		"activity_points":       actPts,
		"used_points":           usedPts,
		"available_points":      available,
		"passed_quizzes":        passedQuizzes,
		"progress":              progress,
		"activity_points_limit": limit,
		// 兼容旧字段
		"total_score": quizScore,
	})
}

// GetUserRedemptions 获取当前用户的兑换/积分记录（含所有类型）
func GetUserRedemptions(c *gin.Context) {
	userID := c.GetUint("user_id")
	var records []models.Redemption
	config.DB.Where("user_id=? AND status != 'failed'", userID).Order("created_at DESC").Find(&records)
	c.JSON(http.StatusOK, gin.H{"records": records})
}

// GetAllRedemptions 管理员获取所有兑换记录
func GetAllRedemptions(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	search := c.Query("search")
	status := c.Query("status")
	recordType := c.Query("type")
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	var records []models.Redemption
	var total int64
	query := config.DB.Model(&models.Redemption{})
	if search != "" {
		query = query.Where("employee_id LIKE ? OR user_name LIKE ? OR product_name LIKE ?",
			"%"+search+"%", "%"+search+"%", "%"+search+"%")
	}
	if status != "" {
		query = query.Where("status=?", status)
	}
	if recordType != "" {
		query = query.Where("type=?", recordType)
	}
	query.Count(&total)
	query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&records)
	c.JSON(http.StatusOK, gin.H{
		"records": records, "total": total, "page": page, "page_size": pageSize,
	})
}

// ========== 统计 API ==========

// GetStats 返回后台统计聚合数据，替代前端大分页请求
func GetStats(c *gin.Context) {
	// 总用户数（非管理员）
	var totalUsers int64
	config.DB.Model(&models.User{}).Where("is_admin = ?", false).Count(&totalUsers)

	// 全通关用户数（5关全部通过）
	var allPassedUsers int64
	config.DB.Raw(`
		SELECT COUNT(*) FROM (
			SELECT user_id FROM scores
			WHERE score = 100 AND deleted_at IS NULL
			GROUP BY user_id
			HAVING COUNT(DISTINCT quiz_index) >= 5
		) t
	`).Scan(&allPassedUsers)

	// 已参与闯关人数（至少通过1关）
	var participatedUsers int64
	config.DB.Raw(`
		SELECT COUNT(DISTINCT user_id) FROM scores
		WHERE score = 100 AND deleted_at IS NULL
	`).Scan(&participatedUsers)

	// 平均通关数（所有参与者的平均通过关卡数）
	var avgPassedQuizzes float64
	config.DB.Raw(`
		SELECT COALESCE(AVG(cnt), 0) FROM (
			SELECT COUNT(DISTINCT quiz_index) as cnt FROM scores
			WHERE score = 100 AND deleted_at IS NULL
			GROUP BY user_id
		) t
	`).Scan(&avgPassedQuizzes)

	// 总积分发放量（活动 + 答题）
	var totalActivityPts struct{ Total int }
	config.DB.Model(&models.Redemption{}).Select("COALESCE(SUM(points),0) as total").
		Where("type='activity' AND status='success'").Scan(&totalActivityPts)

	var totalQuizPts struct{ Total int }
	config.DB.Model(&models.Redemption{}).Select("COALESCE(SUM(points),0) as total").
		Where("type='quiz' AND status='success'").Scan(&totalQuizPts)

	// 总兑换次数和消耗积分
	var totalRedeemCount int64
	config.DB.Model(&models.Redemption{}).Where("type='redeem' AND status='success'").Count(&totalRedeemCount)

	var totalRedeemPts struct{ Total int }
	config.DB.Model(&models.Redemption{}).Select("COALESCE(SUM(points),0) as total").
		Where("type='redeem' AND status='success'").Scan(&totalRedeemPts)

	// 各办公地点用户数
	type OfficeCount struct {
		Office string `json:"office"`
		Count  int64  `json:"count"`
	}
	var officeCounts []OfficeCount
	config.DB.Model(&models.User{}).
		Select("office, COUNT(*) as count").
		Where("is_admin = ?", false).
		Group("office").
		Scan(&officeCounts)

	// 线下活动积分上限
	limit := getActivityPointsLimit()

	c.JSON(http.StatusOK, gin.H{
		"total_users":           totalUsers,
		"participated_users":    participatedUsers,
		"avg_passed_quizzes":    math.Round(avgPassedQuizzes*10) / 10,
		"all_passed_users":      allPassedUsers,
		"total_activity_pts":   totalActivityPts.Total,
		"total_quiz_pts":       totalQuizPts.Total,
		"total_redeem_count":   totalRedeemCount,
		"total_redeem_pts":     totalRedeemPts.Total,
		"office_counts":        officeCounts,
		"activity_points_limit": limit,
	})
}

// splitQRDataN 按 | 分割，最多 n 段
func splitQRDataN(data string, n int) []string {
	if n <= 0 {
		return []string{data}
	}
	parts := make([]string, 0, n)
	remaining := data
	for i := 0; i < n-1; i++ {
		idx := strings.Index(remaining, "|")
		if idx < 0 {
			break
		}
		parts = append(parts, remaining[:idx])
		remaining = remaining[idx+1:]
	}
	parts = append(parts, remaining)
	return parts
}
