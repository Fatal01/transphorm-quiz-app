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

// ImportScores 批量导入分数（CSV），自动支持 GBK/UTF-8 编码
func ImportScores(c *gin.Context) {
	quizIndexStr := c.Param("quiz_index")
	quizIndex, err := strconv.Atoi(quizIndexStr)
	if err != nil || quizIndex < 1 || quizIndex > 5 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "问卷序号无效（1-5）"})
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请上传CSV文件"})
		return
	}

	// 自动检测编码并转换为 UTF-8
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
			if len(record) >= 2 && (record[0] == "工号" || record[0] == "employee_id" || record[0] == "EmployeeID") {
				continue
			}
		}

		if len(record) < 2 {
			failCount++
			continue
		}

		employeeID := strings.TrimSpace(record[0])
		scoreStr := strings.TrimSpace(record[1])

		if employeeID == "" {
			failCount++
			continue
		}

		score, err := strconv.Atoi(scoreStr)
		if err != nil {
			failCount++
			errList = append(errList, "分数格式错误: "+employeeID+" = "+scoreStr)
			continue
		}

		// 查找用户
		var user models.User
		if err := config.DB.Where("employee_id = ?", employeeID).First(&user).Error; err != nil {
			failCount++
			errList = append(errList, "用户不存在: "+employeeID)
			continue
		}

		// 更新或创建分数记录
		var existing models.Score
		result := config.DB.Where("user_id = ? AND quiz_index = ?", user.ID, quizIndex).First(&existing)
		if result.Error != nil {
			config.DB.Create(&models.Score{
				UserID:     user.ID,
				EmployeeID: employeeID,
				QuizIndex:  quizIndex,
				Score:      score,
			})
		} else {
			config.DB.Model(&existing).Update("score", score)
		}
		successCount++
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    successCount,
		"fail":       failCount,
		"errors":     errList,
		"quiz_index": quizIndex,
		"message":    "分数导入完成",
	})
}

// GetScores 管理员获取所有分数
func GetScores(c *gin.Context) {
	quizIndexStr := c.Query("quiz_index")
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
		UserID     uint   `json:"user_id"`
		EmployeeID string `json:"employee_id"`
		Name       string `json:"name"`
		Quiz1      int    `json:"quiz_1"`
		Quiz2      int    `json:"quiz_2"`
		Quiz3      int    `json:"quiz_3"`
		Quiz4      int    `json:"quiz_4"`
		Quiz5      int    `json:"quiz_5"`
		Total      int    `json:"total"`
	}

	var users []models.User
	var total int64
	query := config.DB.Model(&models.User{}).Where("is_admin = ?", false)
	if search != "" {
		query = query.Where("employee_id LIKE ? OR name LIKE ?", "%"+search+"%", "%"+search+"%")
	}
	query.Count(&total)
	query.Offset((page - 1) * pageSize).Limit(pageSize).Find(&users)

	_ = quizIndexStr

	var rows []ScoreRow
	for _, u := range users {
		var scores []models.Score
		config.DB.Where("user_id = ?", u.ID).Find(&scores)

		scoreMap := map[int]int{}
		for _, s := range scores {
			scoreMap[s.QuizIndex] = s.Score
		}

		t := scoreMap[1] + scoreMap[2] + scoreMap[3] + scoreMap[4] + scoreMap[5]
		rows = append(rows, ScoreRow{
			UserID:     u.ID,
			EmployeeID: u.EmployeeID,
			Name:       u.Name,
			Quiz1:      scoreMap[1],
			Quiz2:      scoreMap[2],
			Quiz3:      scoreMap[3],
			Quiz4:      scoreMap[4],
			Quiz5:      scoreMap[5],
			Total:      t,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"scores":    rows,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// UpdateScore 手动修改单个用户某问卷分数
func UpdateScore(c *gin.Context) {
	var req struct {
		EmployeeID string `json:"employee_id" binding:"required"`
		QuizIndex  int    `json:"quiz_index" binding:"required"`
		Score      int    `json:"score"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	if req.QuizIndex < 1 || req.QuizIndex > 5 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "问卷序号无效"})
		return
	}

	var user models.User
	if err := config.DB.Where("employee_id = ?", req.EmployeeID).First(&user).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	var existing models.Score
	result := config.DB.Where("user_id = ? AND quiz_index = ?", user.ID, req.QuizIndex).First(&existing)
	if result.Error != nil {
		config.DB.Create(&models.Score{
			UserID:     user.ID,
			EmployeeID: req.EmployeeID,
			QuizIndex:  req.QuizIndex,
			Score:      req.Score,
		})
	} else {
		config.DB.Model(&existing).Update("score", req.Score)
	}

	c.JSON(http.StatusOK, gin.H{"message": "分数更新成功"})
}
