package handlers

import (
	"encoding/csv"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"quiz-app/config"
	"quiz-app/middleware"
	"quiz-app/models"
)

// Login 用户登录
func Login(c *gin.Context) {
	var req struct {
		EmployeeID string `json:"employee_id" binding:"required"`
		Name       string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入工号和姓名"})
		return
	}

	req.EmployeeID = strings.TrimSpace(req.EmployeeID)
	req.Name = strings.TrimSpace(req.Name)

	var user models.User
	result := config.DB.Where("employee_id = ? AND name = ?", req.EmployeeID, req.Name).First(&user)
	if result.Error != nil {
		// 检查是否是管理员（必须工号和姓名都匹配）
		if req.EmployeeID == "admin" {
			var adminUser models.User
			config.DB.Where("employee_id = ? AND name = ? AND is_admin = ?", "admin", req.Name, true).First(&adminUser)
			if adminUser.ID > 0 {
				token, err := middleware.GenerateToken(adminUser.ID, adminUser.EmployeeID, adminUser.Name, true)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "生成token失败"})
					return
				}
				c.JSON(http.StatusOK, gin.H{
					"token":    token,
					"user":     adminUser,
					"is_admin": true,
				})
				return
			}
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "工号或姓名不正确"})
		return
	}

	token, err := middleware.GenerateToken(user.ID, user.EmployeeID, user.Name, user.IsAdmin)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成token失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token":    token,
		"user":     user,
		"is_admin": user.IsAdmin,
	})
}

// GetProfile 获取当前用户信息和分数
func GetProfile(c *gin.Context) {
	userID := c.GetUint("user_id")

	var user models.User
	config.DB.First(&user, userID)

	// 获取各问卷分数
	var scores []models.Score
	config.DB.Where("user_id = ?", userID).Find(&scores)

	scoreMap := map[int]int{}
	for _, s := range scores {
		scoreMap[s.QuizIndex] = s.Score
	}

	total := 0
	for _, v := range scoreMap {
		total += v
	}

	c.JSON(http.StatusOK, gin.H{
		"user":        user,
		"scores":      scoreMap,
		"total_score": total,
	})
}

// GetAllUsers 管理员获取所有用户
func GetAllUsers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	search := c.Query("search")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var users []models.User
	var total int64

	query := config.DB.Model(&models.User{}).Where("is_admin = ?", false)
	if search != "" {
		query = query.Where("employee_id LIKE ? OR name LIKE ?", "%"+search+"%", "%"+search+"%")
	}

	query.Count(&total)
	query.Offset((page - 1) * pageSize).Limit(pageSize).Find(&users)

	// 获取每个用户的总分
	type UserWithScore struct {
		models.User
		TotalScore int `json:"total_score"`
	}
	var result []UserWithScore
	for _, u := range users {
		var sum struct{ Total int }
		config.DB.Model(&models.Score{}).Select("COALESCE(SUM(score), 0) as total").Where("user_id = ?", u.ID).Scan(&sum)
		result = append(result, UserWithScore{User: u, TotalScore: sum.Total})
	}

	c.JSON(http.StatusOK, gin.H{
		"users":     result,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// ImportUsers 批量导入用户（CSV）
func ImportUsers(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请上传CSV文件"})
		return
	}

	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "文件读取失败"})
		return
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.TrimLeadingSpace = true

	var successCount, failCount int
	var errors []string
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
			errors = append(errors, "行格式错误: "+strings.Join(record, ","))
			continue
		}

		employeeID := strings.TrimSpace(record[0])
		name := strings.TrimSpace(record[1])

		if employeeID == "" || name == "" {
			failCount++
			continue
		}

		var user models.User
		result := config.DB.Where("employee_id = ?", employeeID).First(&user)
		if result.Error != nil {
			// 新建用户
			newUser := models.User{
				EmployeeID: employeeID,
				Name:       name,
			}
			if err := config.DB.Create(&newUser).Error; err != nil {
				failCount++
				errors = append(errors, "创建失败: "+employeeID)
			} else {
				successCount++
			}
		} else {
			// 更新姓名
			config.DB.Model(&user).Update("name", name)
			successCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": successCount,
		"fail":    failCount,
		"errors":  errors,
		"message": "导入完成",
	})
}

// DeleteUser 删除用户
func DeleteUser(c *gin.Context) {
	id := c.Param("id")
	var user models.User
	if err := config.DB.First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}
	config.DB.Delete(&user)
	config.DB.Where("user_id = ?", user.ID).Delete(&models.Score{})
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// UpdateUser 更新用户信息
func UpdateUser(c *gin.Context) {
	id := c.Param("id")
	var user models.User
	if err := config.DB.First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	config.DB.Model(&user).Update("name", req.Name)
	c.JSON(http.StatusOK, gin.H{"message": "更新成功", "user": user})
}

// ExportUsers 导出用户列表和分数
func ExportUsers(c *gin.Context) {
	var users []models.User
	config.DB.Where("is_admin = ?", false).Find(&users)

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=users_scores.csv")

	w := csv.NewWriter(c.Writer)
	w.Write([]string{"工号", "姓名", "问卷1", "问卷2", "问卷3", "问卷4", "问卷5", "总分"})

	for _, u := range users {
		var scores []models.Score
		config.DB.Where("user_id = ?", u.ID).Find(&scores)

		scoreMap := map[int]int{}
		for _, s := range scores {
			scoreMap[s.QuizIndex] = s.Score
		}

		total := 0
		for _, v := range scoreMap {
			total += v
		}

		row := []string{
			u.EmployeeID,
			u.Name,
			strconv.Itoa(scoreMap[1]),
			strconv.Itoa(scoreMap[2]),
			strconv.Itoa(scoreMap[3]),
			strconv.Itoa(scoreMap[4]),
			strconv.Itoa(scoreMap[5]),
			strconv.Itoa(total),
		}
		w.Write(row)
	}
	w.Flush()
}
