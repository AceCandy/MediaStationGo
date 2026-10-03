// hongguo-backfill 执行一次匿名 App 目录扫描；显式 --apply 才追加到当前配置的红果库。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/hongguo"
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
	apply := flag.Bool("apply", false, "写入当前配置数据库；默认只扫描")
	category := flag.String("category", "", "仅扫描指定官方分类；默认扫描三个分类")
	flag.Parse()
	categories := hongguo.Categories[:]
	if *category != "" {
		if !hongguo.ValidCategory(*category) {
			return errors.New("分类无效")
		}
		categories = []string{*category}
	}
	var repo *repository.HongGuoRepository
	if *apply {
		cfg, err := config.Load()
		if err != nil {
			return errors.New("无法加载数据库配置")
		}
		db, err := database.Open(cfg, zap.NewNop())
		if err != nil {
			return errors.New("目标数据库连接失败；未执行补录")
		}
		sqlDB, err := db.DB()
		if err != nil {
			return errors.New("目标数据库连接无效")
		}
		defer sqlDB.Close()
		if !db.Migrator().HasTable("hongguo_discoveries") || !db.Migrator().HasTable("hongguo_works") {
			return errors.New("目标库缺少红果目录表；未执行补录")
		}
		repo = repository.New(db).HongGuo
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	client := hongguo.NewClient(nil)
	total, newWorks, classified := 0, 0, 0
	for _, cat := range categories {
		pages, scanned := 0, 0
		err := client.ScanAppCatalog(ctx, cat, func(works []hongguo.Work) error {
			if repo != nil {
				stats, err := repo.SaveAppDiscoveryPage(ctx, cat, works)
				if err != nil {
					return errors.New("目录保存失败；已提交页面保留")
				}
				newWorks += stats.NewWorks
				classified += stats.ClassifiedWorks
			}
			pages++
			scanned += len(works)
			total += len(works)
			if pages%20 == 0 {
				fmt.Printf("分类=%s 页数=%d 去重作品=%d 新增=%d 补齐分类=%d\n", cat, pages, scanned, newWorks, classified)
			}
			return nil
		})
		fmt.Printf("分类=%s 页数=%d 去重作品=%d 到末页=%t\n", cat, pages, scanned, err == nil)
		if err != nil {
			fmt.Printf("已处理=%d 新增=%d 补齐分类=%d\n", total, newWorks, classified)
			return err
		}
	}
	fmt.Printf("扫描结束：分类累计=%d 新增=%d 补齐分类=%d 写入=%t\n", total, newWorks, classified, *apply)
	return nil
}
