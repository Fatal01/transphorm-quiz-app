package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	qrcode "github.com/skip2/go-qrcode"
	"quiz-app/config"
	"quiz-app/models"
)

// ========== 商品管理 API ==========

// GetProducts 获取商品列表（公开）
func GetProducts(c *gin.Context) {
	var products []models.Product
	query := config.DB.Where("is_active = ?", true).Order("sort_order ASC, id ASC")
	query.Find(&products)

	c.JSON(http.StatusOK, gin.H{
		"products": products,
	})
}

// GetAllProducts 管理员获取所有商品（含下架）
func GetAllProducts(c *gin.Context) {
	var products []models.Product
	config.DB.Order("sort_order ASC, id ASC").Find(&products)

	c.JSON(http.StatusOK, gin.H{
		"products": products,
	})
}

// CreateProduct 创建商品
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
		Name:        req.Name,
		Description: req.Description,
		Points:      req.Points,
		Stock:       req.Stock,
		IsActive:    req.IsActive,
		SortOrder:   req.SortOrder,
	}
	if err := config.DB.Create(&product).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建商品失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "商品创建成功", "product": product})
}

// UpdateProduct 更新商品
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
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	updates := map[string]interface{}{}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	if req.Points > 0 {
		updates["points"] = req.Points
	}
	updates["stock"] = req.Stock
	updates["sort_order"] = req.SortOrder
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}

	config.DB.Model(&product).Updates(updates)
	config.DB.First(&product, id)

	c.JSON(http.StatusOK, gin.H{"message": "商品更新成功", "product": product})
}

// DeleteProduct 删除商品
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

// UploadProductImage 上传商品图片
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

	imageURL := "/api/static/products/" + filename
	c.JSON(http.StatusOK, gin.H{
		"message": "图片上传成功",
		"url":     imageURL,
	})
}

// ========== 二维码 & 兑换 API ==========

var qrSecret = []byte("quiz-shop-qr-secret-2026")

// GenerateQRCode 生成用户兑换二维码（后端生成base64图片，不依赖前端JS库）
func GenerateQRCode(c *gin.Context) {
	userID := c.GetUint("user_id")
	employeeID := c.GetString("employee_id")
	name := c.GetString("name")

	// 生成带时间戳的签名，5分钟有效
	timestamp := time.Now().Unix()
	payload := fmt.Sprintf("%d|%s|%s|%d", userID, employeeID, name, timestamp)
	mac := hmac.New(sha256.New, qrSecret)
	mac.Write([]byte(payload))
	signature := hex.EncodeToString(mac.Sum(nil))

	qrData := fmt.Sprintf("%d|%s|%s|%d|%s", userID, employeeID, name, timestamp, signature)

	// 后端生成二维码PNG图片
	png, err := qrcode.Encode(qrData, qrcode.Medium, 256)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "二维码生成失败"})
		return
	}
	qrImage := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)

	c.JSON(http.StatusOK, gin.H{
		"qr_data":     qrData,
		"qr_image":    qrImage,
		"employee_id": employeeID,
		"name":        name,
		"expires_in":  300,
	})
}

// RedeemProduct 扫码兑换商品（管理员调用）
func RedeemProduct(c *gin.Context) {
	var req struct {
		QRData    string `json:"qr_data" binding:"required"`
		ProductID uint   `json:"product_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	// 解析二维码数据
	var userID uint
	var employeeID, name, signature string
	var timestamp int64
	n, err := fmt.Sscanf(req.QRData, "%d|%s", &userID, &employeeID)
	if n < 2 || err != nil {
		// 手动解析管道分隔
		parts := splitQRData(req.QRData)
		if len(parts) != 5 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "二维码数据无效"})
			return
		}
		uid, _ := strconv.ParseUint(parts[0], 10, 64)
		userID = uint(uid)
		employeeID = parts[1]
		name = parts[2]
		timestamp, _ = strconv.ParseInt(parts[3], 10, 64)
		signature = parts[4]
	}

	// 验证签名
	payload := fmt.Sprintf("%d|%s|%s|%d", userID, employeeID, name, timestamp)
	mac := hmac.New(sha256.New, qrSecret)
	mac.Write([]byte(payload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "二维码签名验证失败，请重新生成"})
		return
	}

	// 检查是否过期（5分钟）
	if time.Now().Unix()-timestamp > 300 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "二维码已过期，请让用户重新生成"})
		return
	}

	// 查找用户
	var user models.User
	if err := config.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	// 查找商品
	var product models.Product
	if err := config.DB.First(&product, req.ProductID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "商品不存在"})
		return
	}

	if !product.IsActive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "商品已下架"})
		return
	}

	if product.Stock <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "商品库存不足"})
		return
	}

	// 计算用户可用积分（答题总分 - 已消耗积分）
	availablePoints := getUserAvailablePoints(userID)

	if availablePoints < product.Points {
		// 记录失败
		config.DB.Create(&models.Redemption{
			UserID:      user.ID,
			EmployeeID:  user.EmployeeID,
			UserName:    user.Name,
			ProductID:   product.ID,
			ProductName: product.Name,
			Points:      product.Points,
			Status:      "failed",
			Remark:      fmt.Sprintf("积分不足，需要%d分，当前可用%d分", product.Points, availablePoints),
			OperatorID:  c.GetUint("user_id"),
		})
		c.JSON(http.StatusBadRequest, gin.H{
			"error":            "积分不足",
			"required_points":  product.Points,
			"available_points": availablePoints,
		})
		return
	}

	// 执行兑换：扣减库存，记录兑换
	config.DB.Model(&product).Update("stock", product.Stock-1)
	config.DB.Create(&models.Redemption{
		UserID:      user.ID,
		EmployeeID:  user.EmployeeID,
		UserName:    user.Name,
		ProductID:   product.ID,
		ProductName: product.Name,
		Points:      product.Points,
		Status:      "success",
		Remark:      "兑换成功",
		OperatorID:  c.GetUint("user_id"),
	})

	newAvailable := availablePoints - product.Points
	c.JSON(http.StatusOK, gin.H{
		"message":          "兑换成功",
		"product_name":     product.Name,
		"points_cost":      product.Points,
		"remaining_points": newAvailable,
		"user_name":        user.Name,
		"employee_id":      user.EmployeeID,
	})
}

// getUserAvailablePoints 计算用户可用积分 = 答题总分 - 已兑换消耗积分
func getUserAvailablePoints(userID uint) int {
	// 计算答题总分
	var totalScore struct{ Total int }
	config.DB.Model(&models.Score{}).Select("COALESCE(SUM(score), 0) as total").Where("user_id = ?", userID).Scan(&totalScore)

	// 计算已消耗积分
	var usedPoints struct{ Total int }
	config.DB.Model(&models.Redemption{}).Select("COALESCE(SUM(points), 0) as total").Where("user_id = ? AND status = ?", userID, "success").Scan(&usedPoints)

	available := totalScore.Total - usedPoints.Total
	if available < 0 {
		available = 0
	}
	return available
}

// GetUserPoints 获取用户积分信息（用户端）
func GetUserPoints(c *gin.Context) {
	userID := c.GetUint("user_id")

	available := getUserAvailablePoints(userID)

	// 答题总分
	var totalScore struct{ Total int }
	config.DB.Model(&models.Score{}).Select("COALESCE(SUM(score), 0) as total").Where("user_id = ?", userID).Scan(&totalScore)

	// 已消耗积分
	var usedPoints struct{ Total int }
	config.DB.Model(&models.Redemption{}).Select("COALESCE(SUM(points), 0) as total").Where("user_id = ? AND status = ?", userID, "success").Scan(&usedPoints)

	c.JSON(http.StatusOK, gin.H{
		"total_score":      totalScore.Total,
		"used_points":      usedPoints.Total,
		"available_points": available,
	})
}

// GetUserRedemptions 获取用户兑换记录（用户端）
func GetUserRedemptions(c *gin.Context) {
	userID := c.GetUint("user_id")

	var records []models.Redemption
	config.DB.Where("user_id = ?", userID).Order("created_at DESC").Find(&records)

	c.JSON(http.StatusOK, gin.H{
		"records": records,
	})
}

// GetAllRedemptions 管理员获取所有兑换记录
func GetAllRedemptions(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	search := c.Query("search")
	status := c.Query("status")

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
		query = query.Where("status = ?", status)
	}

	query.Count(&total)
	query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&records)

	c.JSON(http.StatusOK, gin.H{
		"records":   records,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// splitQRData 分割二维码数据
func splitQRData(data string) []string {
	var parts []string
	current := ""
	for _, ch := range data {
		if ch == '|' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(ch)
		}
	}
	parts = append(parts, current)
	return parts
}
