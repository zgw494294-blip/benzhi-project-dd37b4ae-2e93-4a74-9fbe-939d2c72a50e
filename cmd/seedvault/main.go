package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"seedvault/internal/httpui"
	"seedvault/internal/store"
	"seedvault/internal/workflow"
)

const defaultAddress = "127.0.0.1:19081"

func main() {
	addr := flag.String("addr", addressFromEnvironment(), "HTTP监听地址")
	data := flag.String("data", "data", "持久化数据目录")
	selfcheck := flag.Bool("selfcheck", false, "执行有界端到端自检")
	flag.Parse()
	if err := validateAddress(*addr); err != nil {
		log.Fatal(err)
	}
	if *selfcheck {
		if err := runSelfcheck(*addr); err != nil {
			log.Fatal("自检失败：", err)
		}
		fmt.Println("自检通过：建档、观测、裁决、复核、封存及凭据验证完整")
		return
	}
	st, err := store.Open(*data)
	if err != nil {
		log.Fatal(err)
	}
	app := httpui.New(workflow.New(st))
	server := &http.Server{Addr: *addr, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()
	log.Printf("种子萌发试验封存台监听 %s", *addr)
	if err = server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func runSelfcheck(addr string) error {
	dir, e := os.MkdirTemp("", "seedvault-selfcheck-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	st, e := store.Open(filepath.Join(dir, "records"))
	if e != nil {
		return e
	}
	server := &http.Server{Handler: httpui.New(workflow.New(st)).Handler(), ReadHeaderTimeout: 2 * time.Second}
	ln, e := net.Listen("tcp", addr)
	if e != nil {
		return e
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(ln) }()
	base := "http://" + addr
	client := &http.Client{Timeout: 3 * time.Second}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		server.Shutdown(ctx)
		<-done
	}()
	var source struct {
		ID      string
		Version int
	}
	if e = post(client, base+"/api/sources", map[string]any{"ScientificName": "Davidia involucrata", "CollectionLocation": "四川卧龙保护样地", "Collector": "自检实验员", "CollectionDate": "2026-08-25", "StorageCondition": "4°C干燥避光", "LotCode": "SELF-001"}, &source); e != nil {
		return fmt.Errorf("建立种源：%w", e)
	}
	var trial struct {
		ID      string
		Version int
	}
	if e = post(client, base+"/api/trials", map[string]any{"SourceID": source.ID, "ProtocolName": "自检萌发协议", "ReplicateCount": 1, "TemperatureCelsius": 22, "ObservationSchedule": []string{"第3天"}}, &trial); e != nil {
		return fmt.Errorf("建立试验：%w", e)
	}
	var observed struct {
		Trial struct{ Version int }
		Issue struct{ ID string }
	}
	if e = post(client, base+"/api/trials/"+trial.ID+"/observations", map[string]any{"ReplicateNo": 1, "ObservedAt": "第3天", "GerminatedCount": 8, "MoldCount": 4, "TemperatureCelsius": 22, "HumidityPercent": 72, "EvidenceRef": "photo:selfcheck-1", "ExpectedVersion": trial.Version}, &observed); e != nil {
		return fmt.Errorf("录入观测：%w", e)
	}
	var decided struct{ Version int }
	if e = post(client, base+"/api/trials/"+trial.ID+"/issues/"+observed.Issue.ID+"/decision", map[string]any{"Reason": "培养皿局部污染", "Disposition": "隔离并保留有效样本", "Evidence": "photo:selfcheck-2", "ExpectedVersion": observed.Trial.Version}, &decided); e != nil {
		return fmt.Errorf("裁决异常：%w", e)
	}
	var reviewed struct{ Version int }
	if e = post(client, base+"/api/trials/"+trial.ID+"/review", map[string]any{"Reviewer": "自检复核员", "State": "approved", "Comment": "观测与裁决证据完整", "Remediation": "无", "ExpectedVersion": decided.Version}, &reviewed); e != nil {
		return fmt.Errorf("提交复核：%w", e)
	}
	var frozen struct {
		Certificate      struct{ ID string }
		Trial            struct{ Version int }
		ArchiveChecklist struct{ Valid bool }
	}
	if e = post(client, base+"/api/trials/"+trial.ID+"/freeze", map[string]any{"Issuer": "自检签发员", "ExpectedVersion": reviewed.Version}, &frozen); e != nil {
		return fmt.Errorf("冻结试验：%w", e)
	}
	if !frozen.ArchiveChecklist.Valid {
		return fmt.Errorf("封存完整性清单未通过")
	}
	var verified struct {
		Valid        bool
		Verification struct {
			SnapshotHashValid, AuditChainValid, CredentialStatusValid, FrozenVersionValid bool
		}
	}
	if e = get(client, base+"/api/certificates/"+frozen.Certificate.ID+"/verify", &verified); e != nil {
		return e
	}
	if !verified.Valid || !verified.Verification.SnapshotHashValid || !verified.Verification.AuditChainValid || !verified.Verification.CredentialStatusValid || !verified.Verification.FrozenVersionValid {
		return fmt.Errorf("凭据验证结果为无效")
	}
	reopened, e := store.Open(filepath.Join(dir, "records"))
	if e != nil {
		return e
	}
	if _, ok := reopened.Trial(trial.ID); !ok {
		return fmt.Errorf("重启重放未恢复试验")
	}
	return nil
}
func post(client *http.Client, url string, input, output any) error {
	b, e := json.Marshal(input)
	if e != nil {
		return e
	}
	r, e := client.Post(url, "application/json", bytes.NewReader(b))
	if e != nil {
		return e
	}
	return decodeResponse(r, output)
}
func get(client *http.Client, url string, output any) error {
	r, e := client.Get(url)
	if e != nil {
		return e
	}
	return decodeResponse(r, output)
}
func decodeResponse(r *http.Response, output any) error {
	defer r.Body.Close()
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		b, _ := io.ReadAll(r.Body)
		return fmt.Errorf("HTTP %d: %s", r.StatusCode, b)
	}
	return json.NewDecoder(r.Body).Decode(output)
}
