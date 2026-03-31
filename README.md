# 卓胜微20周年答题闯关活动系统

## 项目概述

本系统是为卓胜微20周年庆典设计的答题闯关活动平台，包含：

- **H5前端**：员工登录页、活动主页（分数展示、AI助手入口、五关答题入口）
- **后台管理**：员工管理、分数管理、活动配置
- **Go后端**：RESTful API，基于 Gin + GORM + SQLite

---

## 目录结构

```
quiz-app/
├── backend/                  # Go后端
│   ├── main.go               # 主入口
│   ├── config/               # 数据库配置
│   ├── models/               # 数据模型
│   ├── handlers/             # API处理器
│   ├── middleware/           # JWT中间件
│   ├── static/               # 静态文件（背景图等）
│   ├── public/
│   │   ├── app/              # H5前端页面
│   │   │   ├── login.html    # 登录页
│   │   │   └── index.html    # 主页
│   │   └── admin/
│   │       └── index.html    # 后台管理页
│   ├── quiz.db               # SQLite数据库（运行后自动生成）
│   └── quiz-server           # 编译后的可执行文件
├── sample_users.csv          # 员工导入示例CSV
├── sample_scores.csv         # 分数导入示例CSV
├── start.sh                  # 启动脚本
├── build.sh                  # 编译脚本
└── README.md
```

---

## 快速启动

### 环境要求

- Go 1.22+
- GCC（用于编译 SQLite 驱动）
- Linux/macOS

### 启动步骤

```bash
# 1. 编译
./build.sh

# 2. 启动服务（默认端口 8080）
./start.sh

# 或指定端口
PORT=9090 ./start.sh
```

### 访问地址

| 页面 | 地址 |
|------|------|
| 员工登录页 | `http://localhost:8080/app/login.html` |
| 活动主页 | `http://localhost:8080/app/index.html` |
| 后台管理 | `http://localhost:8080/admin/index.html` |

---

## 默认账号

| 角色 | 工号 | 姓名 |
|------|------|------|
| 管理员 | `admin` | `管理员` |

> 管理员登录后自动跳转至后台管理页面

---

## CSV文件格式

### 员工导入（`sample_users.csv`）

```csv
工号,姓名
EMP001,张三
EMP002,李四
```

- 支持约1500人批量导入
- 工号已存在时自动更新姓名

### 分数导入（`sample_scores.csv`）

```csv
工号,分数
EMP001,18
EMP002,20
```

- 每个关卡单独上传一个CSV
- 分数范围：0-100
- 重复上传会覆盖已有分数

---

## API接口文档

### 公开接口

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/login` | 员工登录 |
| GET | `/api/config` | 获取活动配置 |

### 员工接口（需登录）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/user/profile` | 获取个人信息和分数 |

### 管理员接口（需管理员权限）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/users` | 获取员工列表 |
| POST | `/api/admin/users/import` | 批量导入员工CSV |
| DELETE | `/api/admin/users/:id` | 删除员工 |
| GET | `/api/admin/users/export` | 导出员工和分数 |
| GET | `/api/admin/scores` | 查询分数 |
| POST | `/api/admin/scores/import/:quiz_index` | 按关卡导入分数CSV |
| PUT | `/api/admin/scores` | 手动修改分数 |
| POST | `/api/admin/config` | 更新活动配置 |
| POST | `/api/admin/config/background` | 上传背景图片 |

---

## 后台配置说明

在后台管理页面的「活动配置」中可设置：

1. **AI问答助手链接**：填入外链URL，员工点击后跳转
2. **五个关卡的问卷链接**：填入问卷外链URL
3. **五个关卡的开放时间**：格式 `2024-01-01 09:00`，到时间后自动开放
4. **背景图片**：上传自定义背景图（JPG/PNG/WebP）

---

## 分数计算规则

- 总分 = 关卡1分数 + 关卡2分数 + 关卡3分数 + 关卡4分数 + 关卡5分数
- 满分：100分
- 分数由管理员通过CSV批量导入，或手动修改

---

## 部署建议

### Nginx反向代理配置

```nginx
server {
    listen 80;
    server_name your-domain.com;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

### 使用 systemd 管理服务

```ini
[Unit]
Description=Quiz App Service
After=network.target

[Service]
Type=simple
WorkingDirectory=/path/to/quiz-app/backend
ExecStart=/path/to/quiz-app/backend/quiz-server
Restart=always
Environment=PORT=8080

[Install]
WantedBy=multi-user.target
```
