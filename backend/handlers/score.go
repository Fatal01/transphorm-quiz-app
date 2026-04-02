package handlers

import (
	"bytes"
	"encoding/csv"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
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

	var successCount, failCount int
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

		// 检查是否5关全部通过，若是则写入答题奖励积分（幂等：只写一次）
		checkAndGrantQuizBonus(user)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    successCount,
		"fail":       failCount,
		"errors":     errList,
		"quiz_index": quizIndex,
		"message":    "通过名单导入完成",
	})
}

// checkAndGrantQuizBonus 检查用户是否5关全通过，若是则写入20分答题奖励（幂等）
func checkAndGrantQuizBonus(user models.User) {
	var passedCount int64
	config.DB.Model(&models.Score{}).Where("user_id = ? AND score = 100", user.ID).Count(&passedCount)
	if passedCount < 5 {
		return
	}

	// 幂等检查：是否已经发放过答题奖励
	var existing int64
	config.DB.Model(&models.Redemption{}).
		Where("user_id = ? AND type = 'quiz' AND status = 'success'", user.ID).
		Count(&existing)
	if existing > 0 {
		return
	}

	// 写入答题奖励积分记录
	config.DB.Create(&models.Redemption{
		UserID:      user.ID,
		EmployeeID:  user.EmployeeID,
		UserName:    user.Name,
		ProductID:   0,
		ProductName: "全通关答题奖励",
		Points:      20,
		Status:      "success",
		Type:        "quiz",
		Remark:      "5关全部通过，自动发放20积分奖励",
	})
}

// GetScores 管理员获取所有用户通过状态
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

	type ScoreRow struct {
		UserID      uint   `json:"user_id"`
		EmployeeID  string `json:"employee_id"`
		Name        string `json:"name"`
		Office      string `json:"office"`
		Quiz1       bool   `json:"quiz_1"` // true=通过
		Quiz2       bool   `json:"quiz_2"`
		Quiz3       bool   `json:"quiz_3"`
		Quiz4       bool   `json:"quiz_4"`
		Quiz5       bool   `json:"quiz_5"`
		PassedCount int    `json:"passed_count"`
		QuizScore   int    `json:"quiz_score"` // 答题积分（全通过得20）
	}

	var users []models.User
	var total int64
	query := config.DB.Model(&models.User{}).Where("is_admin = ?", false)
	if search != "" {
		query = query.Where("employee_id LIKE ? OR name LIKE ?", "%"+search+"%", "%"+search+"%")
	}
	query.Count(&total)
	query.Offset((page - 1) * pageSize).Limit(pageSize).Find(&users)

	var rows []ScoreRow
	for _, u := range users {
		var scores []models.Score
		config.DB.Where("user_id = ?", u.ID).Find(&scores)

		passMap := map[int]bool{}
		for _, s := range scores {
			passMap[s.QuizIndex] = (s.Score == 100)
		}

		passed := 0
		for i := 1; i <= 5; i++ {
			if passMap[i] {
				passed++
			}
		}

		quizScore := 0
		if passed == 5 {
			quizScore = 20
		}

		rows = append(rows, ScoreRow{
			UserID:      u.ID,
			EmployeeID:  u.EmployeeID,
			Name:        u.Name,
			Office:      u.Office,
			Quiz1:       passMap[1],
			Quiz2:       passMap[2],
			Quiz3:       passMap[3],
			Quiz4:       passMap[4],
			Quiz5:       passMap[5],
			PassedCount: passed,
			QuizScore:   quizScore,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"scores":    rows,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
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
