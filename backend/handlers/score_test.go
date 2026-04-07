package handlers

import (
	"fmt"
	"sync"
	"testing"

	"quiz-app/config"
	"quiz-app/models"
)

// TestCheckAndGrantQuizBonusIdempotency 测试幂等性：同一用户多次调用应只发放一次奖励
func TestCheckAndGrantQuizBonusIdempotency(t *testing.T) {
	// 初始化测试数据库连接（假设已配置）
	setupTestDB()
	defer teardownTestDB()

	// 创建测试用户
	user := models.User{
		EmployeeID: "TEST_IDEMPOTENT_001",
		Name:       "Test User Idempotent",
		Office:     "Test Office",
	}
	if err := config.DB.Create(&user).Error; err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// 创建 5 条通过记录
	for i := 1; i <= 5; i++ {
		if err := config.DB.Create(&models.Score{
			UserID:     user.ID,
			EmployeeID: user.EmployeeID,
			QuizIndex:  i,
			Score:      100,
		}).Error; err != nil {
			t.Fatalf("Failed to create score record: %v", err)
		}
	}

	// 第一次调用应该返回 true（新发放奖励）
	result1 := checkAndGrantQuizBonus(user)
	if !result1 {
		t.Error("First call should return true")
	}

	// 第二次调用应该返回 false（已发放过）
	result2 := checkAndGrantQuizBonus(user)
	if result2 {
		t.Error("Second call should return false (already granted)")
	}

	// 验证只有一条 quiz 类型的成功记录
	var count int64
	config.DB.Model(&models.Redemption{}).
		Where("user_id = ? AND type = 'quiz' AND status = 'success'", user.ID).
		Count(&count)

	if count != 1 {
		t.Errorf("Expected 1 quiz record, got %d", count)
	}

	// 验证用户的 quiz_score 字段
	var updatedUser models.User
	config.DB.First(&updatedUser, user.ID)
	if updatedUser.QuizScore != 20 {
		t.Errorf("Expected quiz_score = 20, got %d", updatedUser.QuizScore)
	}

	// 清理测试数据
	config.DB.Delete(&user)
}

// TestCheckAndGrantQuizBonusConcurrency 测试并发安全性：10 个并发请求应只发放一次奖励
func TestCheckAndGrantQuizBonusConcurrency(t *testing.T) {
	setupTestDB()
	defer teardownTestDB()

	// 创建测试用户
	user := models.User{
		EmployeeID: "TEST_CONCURRENT_001",
		Name:       "Test User Concurrent",
		Office:     "Test Office",
	}
	if err := config.DB.Create(&user).Error; err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// 创建 5 条通过记录
	for i := 1; i <= 5; i++ {
		if err := config.DB.Create(&models.Score{
			UserID:     user.ID,
			EmployeeID: user.EmployeeID,
			QuizIndex:  i,
			Score:      100,
		}).Error; err != nil {
			t.Fatalf("Failed to create score record: %v", err)
		}
	}

	// 并发调用 10 次
	const concurrency = 10
	var wg sync.WaitGroup
	results := make([]bool, concurrency)
	resultMutex := sync.Mutex{}

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			result := checkAndGrantQuizBonus(user)
			resultMutex.Lock()
			results[idx] = result
			resultMutex.Unlock()
		}(i)
	}

	wg.Wait()

	// 统计返回 true 的次数，应该只有 1 次
	trueCount := 0
	for _, r := range results {
		if r {
			trueCount++
		}
	}

	if trueCount != 1 {
		t.Errorf("Expected exactly 1 true result from %d concurrent calls, got %d", concurrency, trueCount)
	}

	// 验证只有一条 quiz 类型的成功记录
	var count int64
	config.DB.Model(&models.Redemption{}).
		Where("user_id = ? AND type = 'quiz' AND status = 'success'", user.ID).
		Count(&count)

	if count != 1 {
		t.Errorf("Expected 1 quiz record, got %d", count)
	}

	// 验证用户的 quiz_score 字段
	var updatedUser models.User
	config.DB.First(&updatedUser, user.ID)
	if updatedUser.QuizScore != 20 {
		t.Errorf("Expected quiz_score = 20, got %d", updatedUser.QuizScore)
	}

	// 清理测试数据
	config.DB.Delete(&user)
}

// TestCheckAndGrantQuizBonusWithActivityPoints 测试奖励发放后的积分计算（包含活动积分）
func TestCheckAndGrantQuizBonusWithActivityPoints(t *testing.T) {
	setupTestDB()
	defer teardownTestDB()

	// 创建测试用户
	user := models.User{
		EmployeeID: "TEST_ACTIVITY_001",
		Name:       "Test User Activity",
		Office:     "Test Office",
	}
	if err := config.DB.Create(&user).Error; err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// 创建 5 条通过记录
	for i := 1; i <= 5; i++ {
		if err := config.DB.Create(&models.Score{
			UserID:     user.ID,
			EmployeeID: user.EmployeeID,
			QuizIndex:  i,
			Score:      100,
		}).Error; err != nil {
			t.Fatalf("Failed to create score record: %v", err)
		}
	}

	// 先添加一些活动积分
	if err := config.DB.Create(&models.Redemption{
		UserID:      user.ID,
		EmployeeID:  user.EmployeeID,
		UserName:    user.Name,
		ProductID:   0,
		ProductName: "线下活动积分",
		Points:      30,
		Status:      "success",
		Type:        "activity",
		Remark:      "Test activity points",
	}).Error; err != nil {
		t.Fatalf("Failed to create activity record: %v", err)
	}

	// 调用奖励发放函数
	result := checkAndGrantQuizBonus(user)
	if !result {
		t.Error("checkAndGrantQuizBonus should return true")
	}

	// 验证用户的积分计算
	var updatedUser models.User
	config.DB.First(&updatedUser, user.ID)

	expectedQuizScore := 20
	expectedPoints := 20 + 30 // quiz_score + activity_points
	if updatedUser.QuizScore != expectedQuizScore {
		t.Errorf("Expected quiz_score = %d, got %d", expectedQuizScore, updatedUser.QuizScore)
	}
	if updatedUser.Points != expectedPoints {
		t.Errorf("Expected points = %d, got %d", expectedPoints, updatedUser.Points)
	}

	// 清理测试数据
	config.DB.Delete(&user)
}

// TestCheckAndGrantQuizBonusWithRedemption 测试奖励发放后的积分计算（包含兑换消耗）
func TestCheckAndGrantQuizBonusWithRedemption(t *testing.T) {
	setupTestDB()
	defer teardownTestDB()

	// 创建测试用户
	user := models.User{
		EmployeeID: "TEST_REDEEM_001",
		Name:       "Test User Redeem",
		Office:     "Test Office",
	}
	if err := config.DB.Create(&user).Error; err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// 创建 5 条通过记录
	for i := 1; i <= 5; i++ {
		if err := config.DB.Create(&models.Score{
			UserID:     user.ID,
			EmployeeID: user.EmployeeID,
			QuizIndex:  i,
			Score:      100,
		}).Error; err != nil {
			t.Fatalf("Failed to create score record: %v", err)
		}
	}

	// 先添加活动积分和兑换消耗
	if err := config.DB.Create(&models.Redemption{
		UserID:      user.ID,
		EmployeeID:  user.EmployeeID,
		UserName:    user.Name,
		ProductID:   0,
		ProductName: "线下活动积分",
		Points:      50,
		Status:      "success",
		Type:        "activity",
		Remark:      "Test activity points",
	}).Error; err != nil {
		t.Fatalf("Failed to create activity record: %v", err)
	}

	if err := config.DB.Create(&models.Redemption{
		UserID:      user.ID,
		EmployeeID:  user.EmployeeID,
		UserName:    user.Name,
		ProductID:   1,
		ProductName: "Test Product",
		Points:      20,
		Status:      "success",
		Type:        "redeem",
		Remark:      "Test redemption",
	}).Error; err != nil {
		t.Fatalf("Failed to create redemption record: %v", err)
	}

	// 调用奖励发放函数
	result := checkAndGrantQuizBonus(user)
	if !result {
		t.Error("checkAndGrantQuizBonus should return true")
	}

	// 验证用户的积分计算
	var updatedUser models.User
	config.DB.First(&updatedUser, user.ID)

	expectedQuizScore := 20
	expectedPoints := 20 + 50 - 20 // quiz_score + activity_points - redeemed_points
	if updatedUser.QuizScore != expectedQuizScore {
		t.Errorf("Expected quiz_score = %d, got %d", expectedQuizScore, updatedUser.QuizScore)
	}
	if updatedUser.Points != expectedPoints {
		t.Errorf("Expected points = %d, got %d", expectedPoints, updatedUser.Points)
	}

	// 清理测试数据
	config.DB.Delete(&user)
}

// TestCheckAndGrantQuizBonusNotAllPassed 测试未全通过的情况
func TestCheckAndGrantQuizBonusNotAllPassed(t *testing.T) {
	setupTestDB()
	defer teardownTestDB()

	// 创建测试用户
	user := models.User{
		EmployeeID: "TEST_NOT_PASSED_001",
		Name:       "Test User Not Passed",
		Office:     "Test Office",
	}
	if err := config.DB.Create(&user).Error; err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// 只创建 4 条通过记录（未全通过）
	for i := 1; i <= 4; i++ {
		if err := config.DB.Create(&models.Score{
			UserID:     user.ID,
			EmployeeID: user.EmployeeID,
			QuizIndex:  i,
			Score:      100,
		}).Error; err != nil {
			t.Fatalf("Failed to create score record: %v", err)
		}
	}

	// 调用奖励发放函数，应该返回 false
	result := checkAndGrantQuizBonus(user)
	if result {
		t.Error("checkAndGrantQuizBonus should return false (not all passed)")
	}

	// 验证没有创建 quiz 类型的记录
	var count int64
	config.DB.Model(&models.Redemption{}).
		Where("user_id = ? AND type = 'quiz'", user.ID).
		Count(&count)

	if count != 0 {
		t.Errorf("Expected 0 quiz records, got %d", count)
	}

	// 清理测试数据
	config.DB.Delete(&user)
}

// TestAutoPassQuizConcurrency 模拟 AutoPassQuiz 接口的并发调用
func TestAutoPassQuizConcurrency(t *testing.T) {
	setupTestDB()
	defer teardownTestDB()

	// 创建测试用户
	user := models.User{
		EmployeeID: "TEST_AUTO_PASS_001",
		Name:       "Test User Auto Pass",
		Office:     "Test Office",
	}
	if err := config.DB.Create(&user).Error; err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// 创建前 4 关的通过记录
	for i := 1; i <= 4; i++ {
		if err := config.DB.Create(&models.Score{
			UserID:     user.ID,
			EmployeeID: user.EmployeeID,
			QuizIndex:  i,
			Score:      100,
		}).Error; err != nil {
			t.Fatalf("Failed to create score record: %v", err)
		}
	}

	// 模拟 AutoPassQuiz 的并发调用（10 个并发请求都尝试通过第 5 关）
	const concurrency = 10
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// 模拟 AutoPassQuiz 的逻辑
			var existing models.Score
			result := config.DB.Where("user_id = ? AND quiz_index = ?", user.ID, 5).First(&existing)
			if result.Error != nil {
				config.DB.Create(&models.Score{
					UserID:     user.ID,
					EmployeeID: user.EmployeeID,
					QuizIndex:  5,
					Score:      100,
				})
			} else if existing.Score != 100 {
				config.DB.Model(&existing).Update("score", 100)
			}

			// 检查是否全通过并发放奖励
			checkAndGrantQuizBonus(user)
		}()
	}

	wg.Wait()

	// 验证只有一条 quiz 类型的成功记录
	var count int64
	config.DB.Model(&models.Redemption{}).
		Where("user_id = ? AND type = 'quiz' AND status = 'success'", user.ID).
		Count(&count)

	if count != 1 {
		t.Errorf("Expected 1 quiz record after concurrent AutoPassQuiz calls, got %d", count)
	}

	// 清理测试数据
	config.DB.Delete(&user)
}

// ============ 辅助函数 ============

// setupTestDB 初始化测试数据库
func setupTestDB() {
	// 这里假设已经在 config 包中初始化了数据库连接
	// 如果需要，可以在这里进行额外的设置
	if config.DB == nil {
		panic("Database not initialized. Please ensure config.DB is set up before running tests.")
	}
}

// teardownTestDB 清理测试数据库
func teardownTestDB() {
	// 这里可以进行清理操作，例如删除测试数据
	// 当前实现中，每个测试用例都会在最后删除自己创建的用户
}

// BenchmarkCheckAndGrantQuizBonus 性能基准测试
func BenchmarkCheckAndGrantQuizBonus(b *testing.B) {
	setupTestDB()
	defer teardownTestDB()

	// 创建测试用户
	user := models.User{
		EmployeeID: "BENCH_001",
		Name:       "Benchmark User",
		Office:     "Test Office",
	}
	if err := config.DB.Create(&user).Error; err != nil {
		b.Fatalf("Failed to create test user: %v", err)
	}

	// 创建 5 条通过记录
	for i := 1; i <= 5; i++ {
		if err := config.DB.Create(&models.Score{
			UserID:     user.ID,
			EmployeeID: user.EmployeeID,
			QuizIndex:  i,
			Score:      100,
		}).Error; err != nil {
			b.Fatalf("Failed to create score record: %v", err)
		}
	}

	// 先调用一次确保奖励已发放
	checkAndGrantQuizBonus(user)

	// 开始性能测试
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		checkAndGrantQuizBonus(user)
	}
	b.StopTimer()

	// 清理测试数据
	config.DB.Delete(&user)
}

// TestQuizBonusDataConsistency 测试数据一致性：验证 User 表和 Redemption 表的数据同步
func TestQuizBonusDataConsistency(t *testing.T) {
	setupTestDB()
	defer teardownTestDB()

	// 创建测试用户
	user := models.User{
		EmployeeID: "TEST_CONSISTENCY_001",
		Name:       "Test User Consistency",
		Office:     "Test Office",
	}
	if err := config.DB.Create(&user).Error; err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// 创建 5 条通过记录
	for i := 1; i <= 5; i++ {
		if err := config.DB.Create(&models.Score{
			UserID:     user.ID,
			EmployeeID: user.EmployeeID,
			QuizIndex:  i,
			Score:      100,
		}).Error; err != nil {
			t.Fatalf("Failed to create score record: %v", err)
		}
	}

	// 发放奖励
	checkAndGrantQuizBonus(user)

	// 获取最新的用户数据
	var updatedUser models.User
	config.DB.First(&updatedUser, user.ID)

	// 从 Redemption 表中查询答题积分
	var quizSum struct{ Total int }
	config.DB.Model(&models.Redemption{}).
		Select("COALESCE(SUM(points),0) as total").
		Where("user_id = ? AND type = 'quiz' AND status = 'success'", user.ID).
		Scan(&quizSum)

	// 验证 User.quiz_score 与 Redemption 表的数据一致
	if updatedUser.QuizScore != quizSum.Total {
		t.Errorf("Data inconsistency: User.quiz_score = %d, but Redemption sum = %d",
			updatedUser.QuizScore, quizSum.Total)
	}

	// 清理测试数据
	config.DB.Delete(&user)
}
