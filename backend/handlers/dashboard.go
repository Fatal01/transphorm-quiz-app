package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"quiz-app/config"
)

// GetDashboard 返回数据大屏所需的全部统计数据
func GetDashboard(c *gin.Context) {

	// ── 1. 基础用户概览 ────────────────────────────────────────────────────────
	var totalUsers int64
	config.DB.Raw("SELECT COUNT(*) FROM users WHERE is_admin=0 AND deleted_at IS NULL").Scan(&totalUsers)

	var participatedUsers int64
	config.DB.Raw(`
		SELECT COUNT(DISTINCT user_id) FROM scores
		WHERE score=100 AND deleted_at IS NULL
	`).Scan(&participatedUsers)

	var allPassedUsers int64
	config.DB.Raw(`
		SELECT COUNT(*) FROM (
			SELECT user_id FROM scores
			WHERE score=100 AND deleted_at IS NULL
			GROUP BY user_id HAVING COUNT(DISTINCT quiz_index)>=5
		) t
	`).Scan(&allPassedUsers)

	var avgPassedQuizzes float64
	config.DB.Raw(`
		SELECT COALESCE(AVG(cnt),0) FROM (
			SELECT COUNT(DISTINCT quiz_index) AS cnt FROM scores
			WHERE score=100 AND deleted_at IS NULL
			GROUP BY user_id
		) t
	`).Scan(&avgPassedQuizzes)

	// ── 2. 积分总览 ────────────────────────────────────────────────────────────
	var totalInitialPts struct{ Total int }
	config.DB.Raw("SELECT COALESCE(SUM(points),0) AS total FROM redemptions WHERE type='initial' AND status='success'").Scan(&totalInitialPts)

	var totalActivityPts struct{ Total int }
	config.DB.Raw("SELECT COALESCE(SUM(points),0) AS total FROM redemptions WHERE type='activity' AND status='success'").Scan(&totalActivityPts)

	var totalQuizPts struct{ Total int }
	config.DB.Raw("SELECT COALESCE(SUM(points),0) AS total FROM redemptions WHERE type='quiz' AND status='success'").Scan(&totalQuizPts)

	var totalRedeemPts struct{ Total int }
	config.DB.Raw("SELECT COALESCE(SUM(points),0) AS total FROM redemptions WHERE type='redeem' AND status='success'").Scan(&totalRedeemPts)

	var totalRedeemCount int64
	config.DB.Raw("SELECT COUNT(*) FROM redemptions WHERE type='redeem' AND status='success'").Scan(&totalRedeemCount)

	// 沉默用户（未参与任何活动和兑换）
	var silentUsers int64
	config.DB.Raw(`
		SELECT COUNT(*) FROM users u
		WHERE u.is_admin=0 AND u.deleted_at IS NULL
		AND NOT EXISTS (
			SELECT 1 FROM redemptions r
			WHERE r.employee_id=u.employee_id
			AND r.type IN ('activity','redeem') AND r.status='success'
		)
	`).Scan(&silentUsers)

	// 积分清零用户（高活跃，已兑换过且积分为0）
	var zeroPointsUsers int64
	config.DB.Raw("SELECT COUNT(*) FROM users WHERE is_admin=0 AND deleted_at IS NULL AND points=0 AND used_points>0").Scan(&zeroPointsUsers)

	// ── 3. 活动参与度排行 ──────────────────────────────────────────────────────
	type ActivityStat struct {
		Name         string `json:"name"`
		Participants int    `json:"participants"`
		ScanCount    int    `json:"scan_count"`
		TotalPoints  int    `json:"total_points"`
	}
	var activityStats []ActivityStat
	config.DB.Raw(`
		SELECT product_name AS name,
		       COUNT(DISTINCT employee_id) AS participants,
		       COUNT(*) AS scan_count,
		       SUM(points) AS total_points
		FROM redemptions
		WHERE type='activity' AND status='success'
		GROUP BY product_name
		ORDER BY participants DESC
		LIMIT 20
	`).Scan(&activityStats)

	// ── 4. 商品兑换排行 ────────────────────────────────────────────────────────
	type ProductStat struct {
		Name        string `json:"name"`
		RedeemCount int    `json:"redeem_count"`
		TotalPoints int    `json:"total_points"`
		Participants int   `json:"participants"`
	}
	var productStats []ProductStat
	config.DB.Raw(`
		SELECT product_name AS name,
		       COUNT(*) AS redeem_count,
		       SUM(points) AS total_points,
		       COUNT(DISTINCT employee_id) AS participants
		FROM redemptions
		WHERE type='redeem' AND status='success'
		GROUP BY product_name
		ORDER BY redeem_count DESC
		LIMIT 20
	`).Scan(&productStats)

	// ── 5. 积分分布 ────────────────────────────────────────────────────────────
	type PointsBucket struct {
		Label string `json:"label"`
		Count int    `json:"count"`
	}
	var pointsDist []PointsBucket
	config.DB.Raw(`
		SELECT label, COUNT(*) AS count FROM (
			SELECT CASE
				WHEN points = 0            THEN '0分'
				WHEN points BETWEEN 1  AND 20  THEN '1-20分'
				WHEN points BETWEEN 21 AND 50  THEN '21-50分'
				WHEN points BETWEEN 51 AND 100 THEN '51-100分'
				WHEN points BETWEEN 101 AND 180 THEN '101-180分'
				ELSE '180分以上'
			END AS label
			FROM users WHERE is_admin=0 AND deleted_at IS NULL
		) t
		GROUP BY label
		ORDER BY FIELD(label,'0分','1-20分','21-50分','51-100分','101-180分','180分以上')
	`).Scan(&pointsDist)

	// ── 6. 各类型参与人数汇总（排除答题）──────────────────────────────────────
	type TypeStat struct {
		Type         string `json:"type"`
		Participants int    `json:"participants"`
		OpCount      int    `json:"op_count"`
		TotalPoints  int    `json:"total_points"`
		FailCount    int    `json:"fail_count"`
		RefundCount  int    `json:"refund_count"`
	}
	var typeStats []TypeStat
	config.DB.Raw(`
		SELECT type,
		       COUNT(DISTINCT CASE WHEN status='success' THEN employee_id END) AS participants,
		       COUNT(*) AS op_count,
		       SUM(CASE WHEN status='success' THEN points ELSE 0 END) AS total_points,
		       SUM(CASE WHEN status='failed'   THEN 1 ELSE 0 END) AS fail_count,
		       SUM(CASE WHEN status='refunded' THEN 1 ELSE 0 END) AS refund_count
		FROM redemptions
		WHERE type != 'quiz'
		GROUP BY type
		ORDER BY FIELD(type,'initial','activity','redeem')
	`).Scan(&typeStats)

	// ── 7. 今日实时数据 ────────────────────────────────────────────────────────
	type TodayStat struct {
		Type         string `json:"type"`
		Participants int    `json:"participants"`
		OpCount      int    `json:"op_count"`
		TotalPoints  int    `json:"total_points"`
	}
	var todayStats []TodayStat
	config.DB.Raw(`
		SELECT type,
		       COUNT(DISTINCT employee_id) AS participants,
		       COUNT(*) AS op_count,
		       SUM(CASE WHEN status='success' THEN points ELSE 0 END) AS total_points
		FROM redemptions
		WHERE DATE(created_at)=CURDATE() AND type!='quiz'
		GROUP BY type
		ORDER BY FIELD(type,'initial','activity','redeem')
	`).Scan(&todayStats)

	// ── 8. 各办公地点分布 ─────────────────────────────────────────────────────
	type OfficeStat struct {
		Office string `json:"office"`
		Count  int    `json:"count"`
	}
	var officeStats []OfficeStat
	config.DB.Raw(`
		SELECT COALESCE(NULLIF(office,''),'未填写') AS office, COUNT(*) AS count
		FROM users WHERE is_admin=0 AND deleted_at IS NULL
		GROUP BY office ORDER BY count DESC
	`).Scan(&officeStats)

	// ── 9. 近7天每日操作趋势 ──────────────────────────────────────────────────
	type DailyTrend struct {
		Date        string `json:"date"`
		ActivityOps int    `json:"activity_ops"`
		RedeemOps   int    `json:"redeem_ops"`
	}
	var dailyTrend []DailyTrend
	config.DB.Raw(`
		SELECT DATE(created_at) AS date,
		       SUM(CASE WHEN type='activity' AND status='success' THEN 1 ELSE 0 END) AS activity_ops,
		       SUM(CASE WHEN type='redeem'   AND status='success' THEN 1 ELSE 0 END) AS redeem_ops
		FROM redemptions
		WHERE created_at >= DATE_SUB(CURDATE(), INTERVAL 6 DAY)
		  AND type IN ('activity','redeem')
		GROUP BY DATE(created_at)
		ORDER BY date ASC
	`).Scan(&dailyTrend)

	c.JSON(http.StatusOK, gin.H{
		// 基础概览
		"total_users":        totalUsers,
		"participated_users": participatedUsers,
		"all_passed_users":   allPassedUsers,
		"avg_passed_quizzes": avgPassedQuizzes,
		"silent_users":       silentUsers,
		"zero_points_users":  zeroPointsUsers,
		// 积分总览
		"total_initial_pts":  totalInitialPts.Total,
		"total_activity_pts": totalActivityPts.Total,
		"total_quiz_pts":     totalQuizPts.Total,
		"total_redeem_pts":   totalRedeemPts.Total,
		"total_redeem_count": totalRedeemCount,
		// 明细统计
		"type_stats":      typeStats,
		"activity_stats":  activityStats,
		"product_stats":   productStats,
		"points_dist":     pointsDist,
		"office_stats":    officeStats,
		"today_stats":     todayStats,
		"daily_trend":     dailyTrend,
	})
}
