package config

import (
	"fmt"
	"log"
	"os"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"quiz-app/models"
)

var DB *gorm.DB

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func InitDB() {
	// MySQL DSN 支持通过环境变量配置
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		host := getEnv("MYSQL_HOST", "127.0.0.1")
		port := getEnv("MYSQL_PORT", "3306")
		user := getEnv("MYSQL_USER", "root")
		password := getEnv("MYSQL_PASSWORD", "quizapp2026")
		dbname := getEnv("MYSQL_DB", "quiz_app")
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			user, password, host, port, dbname)
	}

	log.Printf("Connecting to MySQL database...")
	var err error
	DB, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		log.Fatal("Failed to connect to MySQL database:", err)
	}

	// 配置连接池
	sqlDB, err := DB.DB()
	if err != nil {
		log.Fatal("Failed to get sql.DB:", err)
	}
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetMaxIdleConns(20)

	// 自动迁移
	err = DB.AutoMigrate(
		&models.User{},
		&models.Score{},
		&models.Config{},
		&models.Product{},
		&models.Redemption{},
		&models.Activity{},
	)
	if err != nil {
		log.Fatal("Failed to migrate database:", err)
	}

	// 初始化默认配置
	initDefaultConfig()
	// 创建默认管理员账号
	createDefaultAdmin()

	log.Println("MySQL database initialized successfully")
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
		"ai_assistant_url":      "",
		"background_image":      "/api/static/bg_tech.png",
		"activity_title":        "卓胜微20周年答题闯关活动",
		"activity_points_limit": "0", // 0=不限制，>0=每人线下活动积分上限
	}

	for k, v := range defaults {
		var cfg models.Config
		// 使用实际列名 config_key（Key 字段映射到 config_key 以避免 MySQL 保留字冲突）
		result := DB.Where("config_key = ?", k).First(&cfg)
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
