//go:build ignore
// +build ignore

// sync_user_points.go
//
// 数据清洗脚本：将所有用户的 User 表冗余积分字段与 Redemption 流水表强制对齐。
//
// 使用场景：
//   1. 首次部署积分同步重构后，修复历史数据中可能存在的不一致问题。
//   2. 任何怀疑积分数据不一致时，可作为修复工具运行。
//
// 使用方法：
//   cd backend
//   go run scripts/sync_user_points.go
//
// 注意：
//   - 脚本会打印每个用户的修复情况，请在执行前备份数据库。
//   - 脚本是幂等的，可以安全地多次执行。

package main

import (
	"fmt"
	"log"
	"os"

	"quiz-app/config"
	"quiz-app/models"
)

func main() {
	// 初始化数据库连接
	config.InitDB()

	log.Println("=== 开始同步用户积分冗余字段 ===")

	// 获取所有非管理员用户
	var users []models.User
	if err := config.DB.Where("is_admin = ?", false).Find(&users).Error; err != nil {
		log.Fatalf("查询用户失败: %v", err)
	}

	log.Printf("共找到 %d 个用户，开始逐一同步...\n", len(users))

	fixedCount := 0
	errorCount := 0

	for _, user := range users {
		// 从 Redemption 表计算各类积分
		var quizSum struct{ Total int }
		config.DB.Model(&models.Redemption{}).
			Select("COALESCE(SUM(points),0) as total").
			Where("user_id = ? AND type = 'quiz' AND status = 'success'", user.ID).
			Scan(&quizSum)

		var actSum struct{ Total int }
		config.DB.Model(&models.Redemption{}).
			Select("COALESCE(SUM(points),0) as total").
			Where("user_id = ? AND type = 'activity' AND status = 'success'", user.ID).
			Scan(&actSum)

		var usedSum struct{ Total int }
		config.DB.Model(&models.Redemption{}).
			Select("COALESCE(SUM(points),0) as total").
			Where("user_id = ? AND type = 'redeem' AND status = 'success'", user.ID).
			Scan(&usedSum)

		availablePoints := quizSum.Total + actSum.Total - usedSum.Total
		if availablePoints < 0 {
			availablePoints = 0
		}

		// 检查是否需要修复
		needsFix := user.QuizScore != quizSum.Total ||
			user.ActivityPoints != actSum.Total ||
			user.UsedPoints != usedSum.Total ||
			user.Points != availablePoints

		if needsFix {
			// 执行修复
			err := config.DB.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]interface{}{
				"quiz_score":      quizSum.Total,
				"activity_points": actSum.Total,
				"used_points":     usedSum.Total,
				"points":          availablePoints,
			}).Error

			if err != nil {
				log.Printf("[ERROR] 用户 %s (%s) 修复失败: %v", user.Name, user.EmployeeID, err)
				errorCount++
				continue
			}

			fmt.Printf("[FIXED] 用户 %-10s (%s)\n", user.Name, user.EmployeeID)
			fmt.Printf("        quiz_score:      %d -> %d\n", user.QuizScore, quizSum.Total)
			fmt.Printf("        activity_points: %d -> %d\n", user.ActivityPoints, actSum.Total)
			fmt.Printf("        used_points:     %d -> %d\n", user.UsedPoints, usedSum.Total)
			fmt.Printf("        points:          %d -> %d\n", user.Points, availablePoints)
			fixedCount++
		}
	}

	fmt.Println()
	log.Printf("=== 同步完成 ===")
	log.Printf("总用户数: %d", len(users))
	log.Printf("已修复:   %d", fixedCount)
	log.Printf("无需修复: %d", len(users)-fixedCount-errorCount)
	log.Printf("修复失败: %d", errorCount)

	if errorCount > 0 {
		os.Exit(1)
	}
}
