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
	"quiz-app/middleware"
	"quiz-app/models"
)

// Login 用户登录
func Login(c *gin.Context) {
	var req struct {
		EmployeeID string `json:"employee_id" binding:"required"`
		Name       string `json:"name" binding:"required"`
		Office     string `json:"office"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入工号和姓名"})
		return
	}

	req.EmployeeID = strings.TrimSpace(req.EmployeeID)
	req.Name = strings.TrimSpace(req.Name)
	req.Office = strings.TrimSpace(req.Office)

	var user models.User
	result := config.DB.Where("employee_id = ? AND name = ?", req.EmployeeID, req.Name).First(&user)
	if result.Error != nil {
		// 检查是否是管理员
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

	// 如果提交了办公地点，更新到数据库
	if req.Office != "" {
		config.DB.Model(&user).Update("office", req.Office)
		user.Office = req.Office
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

// UpdateOffice 更新当前用户的办公地点
func UpdateOffice(c *gin.Context) {
	userID := c.GetUint("user_id")

	var req struct {
		Office string `json:"office" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供办公地点"})
		return
	}

	req.Office = strings.TrimSpace(req.Office)

	var user models.User
	if err := config.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	config.DB.Model(&user).Update("office", req.Office)
	c.JSON(http.StatusOK, gin.H{"message": "办公地点已更新", "office": req.Office})
}

// GetProfile 获取当前用户信息和分数
// 注意：total_score 返回的是积分系统中的答题积分（5关全通过=20分），
// 而非各关卡原始分数（0-100）的累加。如需查看完整积分明细，请使用 /api/user/points 接口。
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

	c.JSON(http.StatusOK, gin.H{
		"user":   user,
		"scores": scoreMap,
		// total_score 为积分系统中的答题积分，与 user.quiz_score 一致
		// 不再是各关卡原始分数的累加（最高500），避免前端展示混淆
		"total_score": user.QuizScore,
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

	type UserWithScore struct {
		models.User
		PassedCount int `json:"passed_count"`
	}
	var resultList []UserWithScore
	for _, u := range users {
		var passed int64
		config.DB.Model(&models.Score{}).Where("user_id = ? AND score = 100", u.ID).Count(&passed)
		resultList = append(resultList, UserWithScore{User: u, PassedCount: int(passed)})
	}

	c.JSON(http.StatusOK, gin.H{
		"users":     resultList,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// ImportUsers 批量导入用户（CSV），自动支持 GBK/UTF-8 编码
func ImportUsers(c *gin.Context) {
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
		result := config.DB.Unscoped().Where("employee_id = ?", employeeID).First(&user)
		if result.Error != nil {
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
		} else if user.DeletedAt.Valid {
			config.DB.Unscoped().Delete(&user)
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
			config.DB.Model(&user).Updates(map[string]interface{}{"name": name})
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

// ExportUsers 导出用户列表（含办公地点、通过状态、积分）
func ExportUsers(c *gin.Context) {
	var users []models.User
	config.DB.Where("is_admin = ?", false).Find(&users)

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=users_scores.csv")

	// 写入 UTF-8 BOM，确保 Excel 打开不乱码
	c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})

	w := csv.NewWriter(c.Writer)
		w.Write([]string{
			"工号", "姓名", "办公地点",
			"初创(1)", "挑战(2)", "突破(3)", "上升(4)", "转型(5)",
			"答题积分", "活动积分", "初始积分", "已兑换", "可用积分",
		})

	for _, u := range users {
		// 各关卡通过状态：从 Score 表读取（展示用）
		var scores []models.Score
		config.DB.Where("user_id = ?", u.ID).Find(&scores)
		passMap := map[int]string{}
		for _, s := range scores {
			if s.Score == 100 {
				passMap[s.QuizIndex] = "通过"
			} else {
				passMap[s.QuizIndex] = "未通过"
			}
		}

			// 积分明细：全部从 Redemption 表读取，与后台管理页面和用户端接口数据来源一致
			quizScore, actPts, usedPts, initPts := getUserPointsBreakdown(u.ID)
			available := quizScore + actPts + initPts - usedPts
			if available < 0 {
				available = 0
			}

			row := []string{
				u.EmployeeID,
				u.Name,
				u.Office,
				passMap[1],
				passMap[2],
				passMap[3],
				passMap[4],
				passMap[5],
				strconv.Itoa(quizScore),
				strconv.Itoa(actPts),
				strconv.Itoa(initPts),
				strconv.Itoa(usedPts),
				strconv.Itoa(available),
			}
		w.Write(row)
	}
	w.Flush()
}
