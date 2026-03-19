package web

import "github.com/gin-gonic/gin"

// SetupStaticRoutes wires frontend static assets when available.
//
// 当前 worktree 未包含可嵌入的前端资源，因此这里保持空实现，
// 让 API 路由与测试链路可以独立编译运行；Gin 默认会对未命中路径返回 404。
func SetupStaticRoutes(router *gin.Engine) {
	_ = router
}
