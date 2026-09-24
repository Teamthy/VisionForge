// Command seed inserts development seed data (demo user, project, models).
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"

	"github.com/visionforge/visionforge/apps/api/internal/db"
	"github.com/visionforge/visionforge/apps/api/internal/repository"
	vtypes "github.com/visionforge/visionforge/packages/types"
	"github.com/visionforge/visionforge/packages/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	pg, err := db.Open(cfg.DB)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pg.Close()

	ctx := context.Background()
	repos := repository.New(pg)

	// Demo user (admin).
	hash, _ := bcrypt.GenerateFromPassword([]byte("demo12345!"), bcrypt.DefaultCost)
	demoEmail := "demo@visionforge.local"
	user, err := repos.Users.FindByEmail(ctx, demoEmail)
	if err != nil {
		user, err = repos.Users.Create(ctx, demoEmail, string(hash), "Demo User", vtypes.RoleAdmin)
		if err != nil {
			log.Fatalf("create demo user: %v", err)
		}
		fmt.Println("created demo user:", user.ID, demoEmail, "password: demo12345!")
	} else {
		fmt.Println("demo user already exists:", user.ID)
	}

	// Demo project.
	projects, _, _, _ := repos.Projects.ListByOwner(ctx, user.ID, "", 1)
	if len(projects) == 0 {
		p, err := repos.Projects.Create(ctx, user.ID, "Demo Project", "Auto-created demo project for local development.")
		if err != nil {
			log.Fatalf("create demo project: %v", err)
		}
		fmt.Println("created demo project:", p.ID)
	} else {
		fmt.Println("demo project already exists:", projects[0].ID)
	}

	// Seed built-in model families.
	seedModels := []struct {
		name, desc string
		tt         vtypes.TaskType
		versions   []struct{ ver, art string; runtime vtypes.Runtime }
	}{
		{
			name: "YOLOv8n-COCO", desc: "Ultralytics YOLOv8 nano trained on COCO (object detection).", tt: vtypes.TaskObjectDetection,
			versions: []struct{ ver, art string; runtime vtypes.Runtime }{
				{"1.0.0", "artifacts/yolov8n.onnx", vtypes.RuntimeONNX},
			},
		},
		{
			name: "MobileNetV3-ImageNet", desc: "MobileNetV3-small classification on ImageNet (1000 classes).", tt: vtypes.TaskClassification,
			versions: []struct{ ver, art string; runtime vtypes.Runtime }{
				{"1.0.0", "artifacts/mobilenetv3.onnx", vtypes.RuntimeONNX},
			},
		},
	}
	for _, sm := range seedModels {
		m, err := repos.Models.FindByName(ctx, sm.name)
		if err != nil {
			m, err = repos.Models.Create(ctx, sm.name, sm.desc, sm.tt)
			if err != nil {
				log.Printf("create model %s: %v", sm.name, err)
				continue
			}
			fmt.Println("created model:", m.ID, m.Name)
		}
		existing, _ := repos.ModelVersions.ListByModel(ctx, m.ID)
		existingByVer := map[string]bool{}
		for _, v := range existing {
			existingByVer[v.Version] = true
		}
		for _, v := range sm.versions {
			if existingByVer[v.ver] {
				continue
			}
			mv, err := repos.ModelVersions.Create(ctx, &vtypes.ModelVersion{
				ModelID: m.ID, Version: v.ver, ArtifactURI: v.art, Runtime: v.runtime,
				Status: vtypes.ModelVersionActive, CreatedAt: time.Now().UTC(),
			})
			if err != nil {
				log.Printf("create model version %s/%s: %v", m.Name, v.ver, err)
				continue
			}
			fmt.Println("created model version:", mv.ID, m.Name, v.ver)
		}
	}
	fmt.Println("seed complete")
}
