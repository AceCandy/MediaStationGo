package service

import (
	"encoding/json"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/testdb"
)

func TestPlayerRequestLogSerialNumberRoundTrip(t *testing.T) {
	db, err := testdb.OpenPostgres(t, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	// 超过 JavaScript 安全整数上限，验证接口通过字符串保留完整编号。
	if err := db.Exec(`SELECT setval('player_request_logs_serial_no_seq', 9007199254740993, false)`).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewPlayerRequestLogService(repository.New(db).PlayerLog, nil)
	row := model.PlayerRequestLog{
		RequestedAt: time.Now().UTC(), Method: "GET", Route: "/test", Status: 200,
		PathParams: map[string][]string{}, Headers: map[string][]string{}, Query: map[string][]string{},
	}
	if err := svc.Record(t.Context(), &row); err != nil {
		t.Fatal(err)
	}
	page, err := svc.List(t.Context(), PlayerRequestLogFilter{Month: row.RequestedAt, Page: 1, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].SerialNo != "9007199254740993" {
		t.Fatalf("unexpected log page: %#v", page)
	}
	body, err := json.Marshal(page.Items[0])
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["serial_no"] != "9007199254740993" {
		t.Fatalf("serial_no must be a decimal string: %v", fields["serial_no"])
	}
}
