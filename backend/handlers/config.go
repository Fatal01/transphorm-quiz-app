package handlers

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"quiz-app/config"
	"quiz-app/models"
)

// GetConfig 获取系统配置（公开）
func GetConfig(c *gin.Context) {
	var configs []models.Config
	config.DB.Find(&configs)

	result := map[string]string{}
	for _, cfg := range configs {
		result[cfg.Key] = cfg.Value
	}

	// 计算问卷开放状态（使用北京时间 UTC+8）
	quizStatus := map[int]bool{}
	cstZone := time.FixedZone("CST", 8*3600) // UTC+8 北京时间
	now := time.Now().In(cstZone)
	for i := 1; i <= 5; i++ {
		key := "quiz_" + string(rune('0'+i)) + "_open_time"
		openTimeStr := result[key]
		if openTimeStr == "" {
			quizStatus[i] = false
		} else {
			// 将配置的时间字符串解析为北京时间
			openTime, err := time.ParseInLocation("2006-01-02 15:04", openTimeStr, cstZone)
			if err != nil {
				quizStatus[i] = false
			} else {
				quizStatus[i] = now.After(openTime)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"config":      result,
		"quiz_status": quizStatus,
	})
}

// UpdateConfig 更新系统配置（管理员）
// 注意：Config 模型中 Key 字段映射到数据库列 config_key（避免 MySQL 保留字冲突）
func UpdateConfig(c *gin.Context) {
	var req map[string]string
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	for k, v := range req {
		var cfg models.Config
		// 使用实际列名 config_key 避免 MySQL 保留字冲突
		result := config.DB.Where("config_key = ?", k).First(&cfg)
		if result.Error != nil {
			config.DB.Create(&models.Config{Key: k, Value: v})
		} else {
			config.DB.Model(&cfg).Update("value", v)
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "配置更新成功"})
}

// UploadBackground 上传背景图片
func UploadBackground(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请上传图片文件"})
		return
	}

	// 检查文件类型
	ext := filepath.Ext(file.Filename)
	allowed := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true}
	if !allowed[ext] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "只支持 jpg/png/gif/webp 格式"})
		return
	}

	// 保存文件，支持环境变量指定静态文件目录
	staticDir := os.Getenv("STATIC_DIR")
	if staticDir == "" {
		staticDir = "./static"
	}
	os.MkdirAll(staticDir, 0755)
	savePath := staticDir + "/bg" + ext
	if err := c.SaveUploadedFile(file, savePath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存文件失败"})
		return
	}

	// 更新配置（使用实际列名 config_key）
	imageURL := "/api/static/bg" + ext
	var cfg models.Config
	result := config.DB.Where("config_key = ?", "background_image").First(&cfg)
	if result.Error != nil {
		config.DB.Create(&models.Config{Key: "background_image", Value: imageURL})
	} else {
		config.DB.Model(&cfg).Update("value", imageURL)
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "背景图片上传成功",
		"url":     imageURL,
	})
}
