package models

import (
	"time"

	"gorm.io/gorm"
)

// User 用户模型
type User struct {
	ID              uint           `gorm:"primarykey" json:"id"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
	EmployeeID      string         `gorm:"uniqueIndex;not null;size:50" json:"employee_id"` // 工号
	Name            string         `gorm:"not null;size:100" json:"name"`                   // 姓名
	IsAdmin         bool           `gorm:"default:false" json:"is_admin"`                   // 是否管理员
	TotalScore      int            `gorm:"default:0" json:"total_score"`                    // 总分（答题得分，冗余字段）
	Points          int            `gorm:"default:0" json:"points"`                         // 可用积分（冗余字段，实时同步）
	ActivityPoints  int            `gorm:"default:0" json:"activity_points"`                // 线下活动获得的积分（冗余字段）
	UsedPoints      int            `gorm:"default:0" json:"used_points"`                    // 已兑换消耗的积分（冗余字段）
	QuizScore       int            `gorm:"default:0" json:"quiz_score"`                     // 答题积分（冗余字段，5关全通过=20分）
	Office          string         `gorm:"size:100;default:''" json:"office"`               // 办公地点
}

// Score 分数模型（score=100 表示该关卡满分通过）
type Score struct {
	ID         uint           `gorm:"primarykey" json:"id"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
	UserID     uint           `gorm:"not null;index" json:"user_id"`
	EmployeeID string         `gorm:"not null;size:50;index" json:"employee_id"`
	QuizIndex  int            `gorm:"not null" json:"quiz_index"` // 问卷序号 1-5
	Score      int            `gorm:"default:0" json:"score"`     // 100=通过，0=未通过
	User       User           `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// Config 系统配置
type Config struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Key       string    `gorm:"column:config_key;uniqueIndex;not null;size:100" json:"key"`
	Value     string    `gorm:"type:text" json:"value"`
}

// Product 商品模型
type Product struct {
	ID          uint           `gorm:"primarykey" json:"id"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
	Name        string         `gorm:"not null;size:200" json:"name"`           // 商品名称
	Description string         `gorm:"type:text" json:"description"`            // 商品描述
	Image       string         `gorm:"size:500" json:"image"`                   // 商品图片路径
	Points      int            `gorm:"not null;default:0" json:"points"`        // 兑换所需积分
	Stock       int            `gorm:"not null;default:0" json:"stock"`         // 库存数量
	IsActive    bool           `gorm:"default:true" json:"is_active"`           // 是否上架
	SortOrder   int            `gorm:"default:0" json:"sort_order"`             // 排序
}

// Activity 活动模型（扫码增加积分）
type Activity struct {
	ID          uint           `gorm:"primarykey" json:"id"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
	Name        string         `gorm:"not null;size:200" json:"name"`    // 活动名称
	Description string         `gorm:"type:text" json:"description"`     // 活动描述
	Points      int            `gorm:"not null;default:0" json:"points"` // 增加积分数
	IsActive    bool           `gorm:"default:true" json:"is_active"`    // 是否启用
	SortOrder   int            `gorm:"default:0" json:"sort_order"`      // 排序
}

// Redemption 兑换/积分记录模型
type Redemption struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	UserID      uint      `gorm:"not null;index" json:"user_id"`
	EmployeeID  string    `gorm:"not null;size:50;index" json:"employee_id"`
	UserName    string    `gorm:"not null;size:100" json:"user_name"`
	ProductID   uint      `gorm:"default:0;index" json:"product_id"`
	ProductName string    `gorm:"not null;size:200" json:"product_name"`
	Points      int       `gorm:"not null" json:"points"`                           // 消耗/获得积分
	Status      string    `gorm:"not null;size:20;default:'success'" json:"status"` // success / failed / refunded
	Type        string    `gorm:"not null;size:20;default:'redeem'" json:"type"`    // redeem=兑换商品 / activity=活动积分 / quiz=答题奖励
	Remark      string    `gorm:"size:500" json:"remark"`                           // 备注
	OperatorID  uint      `gorm:"default:0" json:"operator_id"`                     // 操作人ID（管理员）
	User        User      `gorm:"foreignKey:UserID" json:"user,omitempty"`
}
