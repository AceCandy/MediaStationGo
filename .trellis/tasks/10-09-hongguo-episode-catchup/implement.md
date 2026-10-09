# 实施
1. 增加红果缺集检查与 Run/Builder 接入。
2. 复用 enqueue 核心，事务内过滤空位与清理后不存在的 placement，原调用行为不变。
3. 隔离 PostgreSQL 定向及红果下载/刷新 race 回归；go vet、diff检查、独立审查。
4. 更新来源规范及验证记录，清理临时容器；保持未部署。
