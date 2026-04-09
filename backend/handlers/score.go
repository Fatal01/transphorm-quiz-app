package handlers

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"quiz-app/config"
	"quiz-app/models"
)

// ImportScores 批量导入通过名单（CSV：只有工号一列），score=100 表示通过
func ImportScores(c *gin.Context) {
	quizIndexStr := c.Param("quiz_index")
	quizIndex, err := strconv.Atoi(quizIndexStr)
	if err != nil || quizIndex < 1 || quizIndex > 5 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "关卡序号无效（1-5）"})
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请上传CSV文件"})
		return
	}

	utf8Data, err := readCSVFileAsUTF8(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "文件读取失败"})
		return
	}

	reader := csv.NewReader(bytes.NewReader(utf8Data))
	reader.TrimLeadingSpace = true

	var successCount, failCount, bonusGranted int
	var errList []string
	isFirst := true

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			failCount++
			continue
		}

		// 跳过表头
		if isFirst {
			isFirst = false
			if len(record) >= 1 && (record[0] == "工号" || record[0] == "employee_id" || record[0] == "EmployeeID") {
				continue
			}
		}

		if len(record) < 1 {
			failCount++
			continue
		}

		employeeID := strings.TrimSpace(record[0])
		if employeeID == "" {
			failCount++
			continue
		}

		// 查找用户
		var user models.User
		if err := config.DB.Where("employee_id = ?", employeeID).First(&user).Error; err != nil {
			failCount++
			errList = append(errList, "用户不存在: "+employeeID)
			continue
		}

		// 更新或创建分数记录（score=100 表示通过）
		var existing models.Score
		result := config.DB.Where("user_id = ? AND quiz_index = ?", user.ID, quizIndex).First(&existing)
		if result.Error != nil {
			config.DB.Create(&models.Score{
				UserID:     user.ID,
				EmployeeID: employeeID,
				QuizIndex:  quizIndex,
				Score:      100,
			})
		} else {
			config.DB.Model(&existing).Update("score", 100)
		}
		successCount++

		// 检查是否5关全部通过，若是则写入20分答题奖励（幂等）
		if checkAndGrantQuizBonus(user) {
			bonusGranted++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":       successCount,
		"fail":          failCount,
		"errors":        errList,
		"quiz_index":    quizIndex,
		"bonus_granted": bonusGranted,
		"message":       "通过名单导入完成",
	})
}

// checkAndGrantQuizBonus 检查用户是否5关全通过，若是则写入20分答题奖励（幂等）
// 使用事务 + FOR UPDATE 行锁确保幂等性，防止高并发下的竞态条件
// 返回 true 表示本次新发放了奖励，false 表示未发放（可能是未全通过或已发放过）
func checkAndGrantQuizBonus(user models.User) bool {
	txErr := config.DB.Transaction(func(tx *gorm.DB) error {
		// 1. 检查是否 5 关全通过
		var passedCount int64
		tx.Model(&models.Score{}).Where("user_id = ? AND score = 100", user.ID).Count(&passedCount)
		if passedCount < 5 {
			return fmt.Errorf("not all passed")
		}

		// 2. 幂等检查：使用 FOR UPDATE 加行锁，强制并发请求排队
		// 这确保了检查和写入操作的原子性，防止竞态条件
		var existing int64
		tx.Model(&models.Redemption{}).
			Where("user_id = ? AND type = 'quiz' AND status = 'success'", user.ID).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Count(&existing)
		if existing > 0 {
			return fmt.Errorf("already granted")
		}

		// 3. 写入答题奖励积分记录
		if err := tx.Create(&models.Redemption{
			UserID:      user.ID,
			EmployeeID:  user.EmployeeID,
			UserName:    user.Name,
			ProductID:   0,
			ProductName: "全通关答题奖励",
			Points:      20,
			Status:      "success",
			Type:        "quiz",
			Remark:      "5关全部通过，自动发放20积分奖励",
		}).Error; err != nil {
			return fmt.Errorf("failed to create redemption record: %w", err)
		}

		// 4. 全量重算并同步 User 冗余积分字段
		if err := SyncUserPointsTx(tx, user.ID); err != nil {
			return fmt.Errorf("failed to sync user points: %w", err)
		}

		return nil
	})

	// 如果事务返回错误，说明未发放奖励（可能是未全通过或已发放过）
	if txErr != nil {
		log.Printf("[QUIZ_BONUS] User: %s (ID: %d) - %v", user.EmployeeID, user.ID, txErr)
		return false
	}

	// 事务成功，说明本次新发放了奖励
	log.Printf("[QUIZ_BONUS] User: %s (ID: %d) - Bonus granted successfully", user.EmployeeID, user.ID)
	return true
}

// GetScores 管理员获取所有用户通过状态
// scoreRow 是 GetScores 和 GetTopScores 共用的返回结构体
type scoreRow struct {
	UserID      uint   `json:"user_id"`
	EmployeeID  string `json:"employee_id"`
	Name        string `json:"name"`
	Office      string `json:"office"`
	Quiz1       bool   `json:"quiz_1"`
	Quiz2       bool   `json:"quiz_2"`
	Quiz3       bool   `json:"quiz_3"`
	Quiz4       bool   `json:"quiz_4"`
	Quiz5       bool   `json:"quiz_5"`
	PassedCount int    `json:"passed_count"`
	QuizScore   int    `json:"quiz_score"`
}

// buildScoreRows 将用户列表与其关卡通过状态合并为 ScoreRow 列表。
// 使用一次批量查询替代逐用户循环查询，避免 N+1 问题。
func buildScoreRows(users []models.User) []scoreRow {
	if len(users) == 0 {
		return []scoreRow{}
	}

	// 收集所有 userID
	userIDs := make([]uint, len(users))
	for i, u := range users {
		userIDs[i] = u.ID
	}

	// 一次批量查询所有相关 Score 记录
	var allScores []models.Score
	config.DB.Where("user_id IN ?", userIDs).Find(&allScores)

	// 构建 userID -> passMap 的映射
	passMaps := map[uint]map[int]bool{}
	for _, s := range allScores {
		if passMaps[s.UserID] == nil {
			passMaps[s.UserID] = map[int]bool{}
		}
		passMaps[s.UserID][s.QuizIndex] = (s.Score == 100)
	}

	rows := make([]scoreRow, 0, len(users))
	for _, u := range users {
		pm := passMaps[u.ID]
		passed := 0
		for i := 1; i <= 5; i++ {
			if pm[i] {
				passed++
			}
		}
		quizScore := 0
		if passed == 5 {
			quizScore = 20
		}
		rows = append(rows, scoreRow{
			UserID:      u.ID,
			EmployeeID:  u.EmployeeID,
			Name:        u.Name,
			Office:      u.Office,
			Quiz1:       pm[1],
			Quiz2:       pm[2],
			Quiz3:       pm[3],
			Quiz4:       pm[4],
			Quiz5:       pm[5],
			PassedCount: passed,
			QuizScore:   quizScore,
		})
	}
	return rows
}

// GetScores 管理员获取分页成绩列表（支持搜索）
func GetScores(c *gin.Context) {
	search := c.Query("search")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var users []models.User
	var total int64

	// 构建基础查询条件
	baseQuery := config.DB.Model(&models.User{}).Where("is_admin = ?", false)
	if search != "" {
		baseQuery = baseQuery.Where("employee_id LIKE ? OR name LIKE ?", "%"+search+"%", "%"+search+"%")
	}

	// Count 和 Find 分开执行，避免 GORM 内部状态相互干扰
	baseQuery.Count(&total)
	baseQuery.Offset((page - 1) * pageSize).Limit(pageSize).Find(&users)

	c.JSON(http.StatusOK, gin.H{
		"scores":    buildScoreRows(users),
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetTopScores 获取按通关数量降序排列的前 20 名用户（专用于概览页排行榜）
func GetTopScores(c *gin.Context) {
	// 使用单条 SQL 完成聚合和排序，避免 N+1 查询
	// 注意：MySQL 的 MAX(...) = 1 返回的是整数 0/1，不能直接映射到 Go bool，用 int 接收再手动转换
	type rawRow struct {
		ID          uint   `json:"id"`
		EmployeeID  string `json:"employee_id"`
		Name        string `json:"name"`
		Office      string `json:"office"`
		PassedCount int    `json:"passed_count"`
		Quiz1       int    `json:"-"`
		Quiz2       int    `json:"-"`
		Quiz3       int    `json:"-"`
		Quiz4       int    `json:"-"`
		Quiz5       int    `json:"-"`
	}
	type topRow struct {
		ID          uint   `json:"id"`
		EmployeeID  string `json:"employee_id"`
		Name        string `json:"name"`
		Office      string `json:"office"`
		PassedCount int    `json:"passed_count"`
		Quiz1       bool   `json:"quiz_1"`
		Quiz2       bool   `json:"quiz_2"`
		Quiz3       bool   `json:"quiz_3"`
		Quiz4       bool   `json:"quiz_4"`
		Quiz5       bool   `json:"quiz_5"`
	}

	var rawRows []rawRow
	config.DB.Raw(`
		SELECT
			u.id,
			u.employee_id,
			u.name,
			u.office,
			COUNT(CASE WHEN s.score = 100 THEN 1 END) AS passed_count,
			MAX(CASE WHEN s.quiz_index = 1 AND s.score = 100 THEN 1 ELSE 0 END) AS quiz_1,
			MAX(CASE WHEN s.quiz_index = 2 AND s.score = 100 THEN 1 ELSE 0 END) AS quiz_2,
			MAX(CASE WHEN s.quiz_index = 3 AND s.score = 100 THEN 1 ELSE 0 END) AS quiz_3,
			MAX(CASE WHEN s.quiz_index = 4 AND s.score = 100 THEN 1 ELSE 0 END) AS quiz_4,
			MAX(CASE WHEN s.quiz_index = 5 AND s.score = 100 THEN 1 ELSE 0 END) AS quiz_5
		FROM users u
		LEFT JOIN scores s ON s.user_id = u.id AND s.deleted_at IS NULL
		WHERE u.is_admin = 0 AND u.deleted_at IS NULL
		GROUP BY u.id, u.employee_id, u.name, u.office
		ORDER BY passed_count DESC, u.id ASC
		LIMIT 20
	`).Scan(&rawRows)

	// 将整数 0/1 手动转换为 bool
	topUsers := make([]topRow, 0, len(rawRows))
	for _, r := range rawRows {
		topUsers = append(topUsers, topRow{
			ID:          r.ID,
			EmployeeID:  r.EmployeeID,
			Name:        r.Name,
			Office:      r.Office,
			PassedCount: r.PassedCount,
			Quiz1:       r.Quiz1 == 1,
			Quiz2:       r.Quiz2 == 1,
			Quiz3:       r.Quiz3 == 1,
			Quiz4:       r.Quiz4 == 1,
			Quiz5:       r.Quiz5 == 1,
		})
	}

	c.JSON(http.StatusOK, gin.H{"scores": topUsers})
}

// UpdateScore 手动设置单个用户某关卡通过状态（管理员）
func UpdateScore(c *gin.Context) {
	var req struct {
		EmployeeID string `json:"employee_id" binding:"required"`
		QuizIndex  int    `json:"quiz_index" binding:"required"`
		Passed     bool   `json:"passed"` // true=通过(100)，false=未通过(0)
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	if req.QuizIndex < 1 || req.QuizIndex > 5 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "关卡序号无效"})
		return
	}

	var user models.User
	if err := config.DB.Where("employee_id = ?", req.EmployeeID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	score := 0
	if req.Passed {
		score = 100
	}

	var existing models.Score
	result := config.DB.Where("user_id = ? AND quiz_index = ?", user.ID, req.QuizIndex).First(&existing)
	if result.Error != nil {
		config.DB.Create(&models.Score{
			UserID:     user.ID,
			EmployeeID: req.EmployeeID,
			QuizIndex:  req.QuizIndex,
			Score:      score,
		})
	} else {
		config.DB.Model(&existing).Update("score", score)
	}

	// 如果设为通过，检查是否全通关并发放奖励
	if req.Passed {
		checkAndGrantQuizBonus(user)
	}

	c.JSON(http.StatusOK, gin.H{"message": "状态更新成功"})
}

// AutoPassQuiz 问卷星满分跳转后自动标记通关
// 前端传入 quiz_index（关卡序号，从 URL 参数 passed=N 获取）
// 用户身份由 JWT token 确认（已登录用户）
func AutoPassQuiz(c *gin.Context) {
	var req struct {
		QuizIndex int `json:"quiz_index" binding:"required"` // 关卡序号 1-5
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误，需要 quiz_index 字段"})
		return
	}

	if req.QuizIndex < 1 || req.QuizIndex > 5 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "关卡序号无效（1-5）"})
		return
	}

	// 从 JWT 中获取当前登录用户
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	var user models.User
	if err := config.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	quizNames := []string{"", "初创期（2006-2011）", "挑战期（2012-2013）", "突破期（2014-2018）", "上升期（2019-2021）", "转型期（2022-至今）"}
	quizName := quizNames[req.QuizIndex]

	// 干等写入：已通过则直接返回成功
	var existing models.Score
	result := config.DB.Where("user_id = ? AND quiz_index = ?", user.ID, req.QuizIndex).First(&existing)
	if result.Error != nil {
		config.DB.Create(&models.Score{
			UserID:     user.ID,
			EmployeeID: user.EmployeeID,
			QuizIndex:  req.QuizIndex,
			Score:      100,
		})
	} else if existing.Score != 100 {
		config.DB.Model(&existing).Update("score", 100)
	}

	// 检查是否 5 关全通过，发放奖励
	checkAndGrantQuizBonus(user)

	c.JSON(http.StatusOK, gin.H{
		"message":     "通关成功",
		"quiz_index":  req.QuizIndex,
		"quiz_name":   quizName,
		"employee_id": user.EmployeeID,
		"name":        user.Name,
	})
}
