package config

import (
	"log"
	"os"
	"path/filepath"
	"quiz-app/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func InitDB() {
	// 支持通过环境变量指定数据库路径（Railway持久化存储）
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "quiz.db"
	} else {
		// 确保目录存在
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("Warning: could not create db directory: %v", err)
		}
	}
	log.Printf("Using database: %s", dbPath)

	var err error
	DB, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	// 自动迁移
	err = DB.AutoMigrate(&models.User{}, &models.Score{}, &models.Config{}, &models.Product{}, &models.Redemption{}, &models.Activity{})
	if err != nil {
		log.Fatal("Failed to migrate database:", err)
	}

	// 初始化默认配置
	initDefaultConfig()

	// 创建默认管理员账号
	createDefaultAdmin()

	log.Println("Database initialized successfully")
}

func initDefaultConfig() {
	defaults := map[string]string{
		"quiz_1_url":        "",
		"quiz_2_url":        "",
		"quiz_3_url":        "",
		"quiz_4_url":        "",
		"quiz_5_url":        "",
		"quiz_1_open_time":  "",
		"quiz_2_open_time":  "",
		"quiz_3_open_time":  "",
		"quiz_4_open_time":  "",
		"quiz_5_open_time":  "",
		"ai_assistant_url":  "",
		"background_image":  "/api/static/bg_tech.png",
		"activity_title":    "卓胜微20周年答题闯关活动",
	}

	for k, v := range defaults {
		var cfg models.Config
		result := DB.Where("key = ?", k).First(&cfg)
		if result.Error != nil {
			DB.Create(&models.Config{Key: k, Value: v})
		}
	}
}

func createDefaultAdmin() {
	var admin models.User
	result := DB.Where("employee_id = ?", "admin").First(&admin)
	if result.Error != nil {
		DB.Create(&models.User{
			EmployeeID: "admin",
			Name:       "管理员",
			IsAdmin:    true,
		})
	}
}
