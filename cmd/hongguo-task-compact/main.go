// hongguo-task-compact 一次性将终态分集执行历史整理为作品摘要；默认仅预览范围。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"go.uber.org/zap"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cutoffFlag := flag.String("cutoff", "", "固定截止时间，RFC3339 格式；创建及结束均早于此时间的旧记录才参与")
	apply := flag.Bool("apply", false, "保留作品摘要后删除旧分集执行；默认只预览，不修改数据库")
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New("不接受位置参数，请使用 -cutoff 和 -apply")
	}
	cutoff, err := time.Parse(time.RFC3339, *cutoffFlag)
	if err != nil || !cutoff.Before(time.Now()) {
		return errors.New("必须提供有效且早于当前时间的 -cutoff")
	}
	cfg, err := config.Load()
	if err != nil {
		return errors.New("无法加载数据库配置")
	}
	// 一次性操作使用非预编译连接；不调用 AutoMigrate，也不启动服务。
	db, err := database.OpenForMigration(cfg, zap.NewNop())
	if err != nil {
		return errors.New("目标数据库连接失败，未执行历史整理")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return errors.New("目标数据库连接无效")
	}
	defer sqlDB.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	repo := repository.New(db).HongGuo
	groups, err := repo.LegacyHongGuoDownloadTaskGroups(ctx, cutoff)
	if err != nil {
		return errors.New("旧执行范围查询失败，未执行历史整理")
	}
	sources := make([]string, 0, len(groups))
	var eligible int
	for id, ids := range groups {
		sources = append(sources, id)
		eligible += len(ids)
	}
	slices.Sort(sources)
	fmt.Printf("截止=%s 作品=%d 可整理旧执行=%d 写入=%t\n", cutoff.Format(time.RFC3339), len(sources), eligible, *apply)
	if !*apply {
		return nil
	}
	var removed int64
	for i, id := range sources {
		ids := groups[id]
		for len(ids) > 0 {
			batch := min(len(ids), 20000)
			n, err := repo.CompactHongGuoDownloadTasks(ctx, id, cutoff, ids[:batch])
			if err != nil {
				fmt.Printf("已整理作品=%d 已删除旧执行=%d\n", i, removed)
				return errors.New("历史整理失败，已提交批次保留；可用相同截止时间重新运行")
			}
			removed += n
			ids = ids[batch:]
		}
		delete(groups, id)
		if (i+1)%500 == 0 {
			fmt.Printf("已整理作品=%d/%d 已删除旧执行=%d\n", i+1, len(sources), removed)
		}
	}
	fmt.Printf("整理完成：作品=%d 已删除旧执行=%d\n", len(sources), removed)
	return nil
}
