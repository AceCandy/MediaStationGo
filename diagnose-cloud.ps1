# MediaStationGo 云盘诊断脚本
# 用于检查云盘配置和媒体库状态

Write-Host "=== MediaStationGo 云盘诊断 ===" -ForegroundColor Cyan
Write-Host ""

# 1. 检查容器状态
Write-Host "[1] 检查容器状态..." -ForegroundColor Yellow
docker ps --filter name=mediastation-go --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
Write-Host ""

# 2. 检查最近的日志（云盘相关）
Write-Host "[2] 检查云盘初始化日志..." -ForegroundColor Yellow
docker logs --tail=500 mediastation-go 2>&1 | Select-String -Pattern "cloud|storage|boot" -CaseSensitive:False | Select-Object -Last 20
Write-Host ""

# 3. 检查CORS配置
Write-Host "[3] 检查CORS配置..." -ForegroundColor Yellow
docker exec mediastation-go cat /data/config.toml 2> | Select-String -Pattern "cors"
Write-Host ""

# 4. 测试API健康状态
Write-Host "[4] 测试API健康状态..." -ForegroundColor Yellow
try {
     = Invoke-WebRequest -Uri "http://localhost:8080/api/health" -TimeoutSec 5 -UseBasicParsing
    Write-Host "API状态: " -ForegroundColor Green
} catch {
    Write-Host "API状态: 无法访问 - " -ForegroundColor Red
}
Write-Host ""

# 5. 检查媒体库配置
Write-Host "[5] 检查是否有云盘媒体库..." -ForegroundColor Yellow
docker logs --tail=1000 mediastation-go 2>&1 | Select-String -Pattern "cloud library|ParseCloudLibraryMount" | Select-Object -Last 10
Write-Host ""

Write-Host "=== 诊断完成 ===" -ForegroundColor Cyan
Write-Host ""
Write-Host "如果看到 'no enabled cloud storage configured'，说明需要先配置云盘存储" -ForegroundColor Yellow
Write-Host "如果看到 'cloud storage ping failed'，说明云盘配置有问题（Cookie过期或网络问题）" -ForegroundColor Yellow
Write-Host "如果看到 'cloud library scan completed'，说明云盘扫描正常" -ForegroundColor Green
