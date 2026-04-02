package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"quiz-app/config"
	"quiz-app/handlers"
	"quiz-app/middleware"
)

func main() {
	// 初始化数据库
	config.InitDB()

	// 支持环境变量指定静态文件目录
	staticDir := os.Getenv("STATIC_DIR")
	if staticDir == "" {
		staticDir = "./static"
	}
	os.MkdirAll(staticDir, 0755)

	r := gin.Default()

	// CORS配置
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length", "Content-Disposition"},
		AllowCredentials: false,
	}))

	// 静态文件服务
	r.Static("/api/static", staticDir)

	// 前端H5页面静态服务
	r.Static("/app", "./public/app")
	r.Static("/admin", "./public/admin")
	r.Static("/shop", "./public/shop")

	// 默认路由 - 重定向到登录页
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/app/")
	})

	api := r.Group("/api")
	{
		// 公开接口
		api.POST("/login", handlers.Login)
		api.GET("/config", handlers.GetConfig)
		api.GET("/products", handlers.GetProducts)           // 公开商品列表
		api.GET("/activities", handlers.GetActiveActivities) // 公开活动列表（供 shop 扫码页使用）

		// 需要登录的接口
		auth := api.Group("/user")
		auth.Use(middleware.AuthRequired())
		{
			auth.GET("/profile", handlers.GetProfile)
			auth.GET("/points", handlers.GetUserPoints)           // 获取积分信息
			auth.PUT("/office", handlers.UpdateOffice)            // 更新办公地点
			auth.GET("/qrcode", handlers.GenerateQRCode)          // 生成兑换二维码
			auth.GET("/redemptions", handlers.GetUserRedemptions)  // 兑换/积分记录
		}

		// 管理员接口
		admin := api.Group("/admin")
		admin.Use(middleware.AdminRequired())
		{
			// 用户管理
			admin.GET("/users", handlers.GetAllUsers)
			admin.POST("/users/import", handlers.ImportUsers)
			admin.DELETE("/users/:id", handlers.DeleteUser)
			admin.PUT("/users/:id", handlers.UpdateUser)
			admin.GET("/users/export", handlers.ExportUsers)

			// 分数/通过状态管理
			admin.GET("/scores", handlers.GetScores)
			admin.POST("/scores/import/:quiz_index", handlers.ImportScores)
			admin.PUT("/scores", handlers.UpdateScore)

			// 系统配置
			admin.GET("/config", handlers.GetConfig)
			admin.POST("/config", handlers.UpdateConfig)
			admin.POST("/config/background", handlers.UploadBackground)

			// 商品管理
			admin.GET("/products", handlers.GetAllProducts)
			admin.POST("/products", handlers.CreateProduct)
			admin.PUT("/products/:id", handlers.UpdateProduct)
			admin.DELETE("/products/:id", handlers.DeleteProduct)
			admin.POST("/products/upload", handlers.UploadProductImage)

			// 活动管理
			admin.GET("/activities", handlers.GetAllActivities)
			admin.POST("/activities", handlers.CreateActivity)
			admin.PUT("/activities/:id", handlers.UpdateActivity)
			admin.DELETE("/activities/:id", handlers.DeleteActivity)
			admin.POST("/activities/scan", handlers.ScanActivity)                // 扫码增加积分
			admin.POST("/activities/:id/refund", handlers.RefundActivity)        // 退回活动积分

				// 兑换管理
				admin.POST("/redeem", handlers.RedeemProduct)                        // 扫码兑换商品
				admin.POST("/redemptions/:id/refund", handlers.RefundRedemption)     // 退回商品兑换
				admin.GET("/redemptions", handlers.GetAllRedemptions)                // 全部记录

				// 统计聚合 API（替代前端大分页请求）
				admin.GET("/stats", handlers.GetStats)
		}
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s", port)
	r.Run(":" + port)
}
