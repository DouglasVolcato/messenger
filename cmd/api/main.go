package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/douglasvolcato/messager-architecture-challenge/internal/db"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/routes"
	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
	"github.com/subosito/gotenv"
)

func main() {
	if err := gotenv.Load(); err != nil {
		panic(err)
	}

	if os.Getenv("JWT_SECRET") == "" {
		if os.Getenv("ENV") == "production" {
			log.Fatal("[config] JWT_SECRET is required in production")
		}

		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			log.Fatalf("[config] could not generate development JWT_SECRET: %v", err)
		}
		if err := os.Setenv("JWT_SECRET", hex.EncodeToString(secret)); err != nil {
			log.Fatalf("[config] could not configure development JWT_SECRET: %v", err)
		}
		log.Print("[config] WARNING: JWT_SECRET is not set; generated an ephemeral development secret. Sessions will be invalidated when the application restarts. Add JWT_SECRET to .env to keep sessions stable.")
	}

	if err := db.InitDB(); err != nil {
		panic(err)
	}
	if err := db.RunMigrations(); err != nil {
		panic(err)
	}

	appVersion, err := utils.GenerateUUID()
	if err != nil {
		panic(err)
	}

	templ := template.New("")
	err = filepath.Walk("internal/views", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(path, "internal/views/pages/")
		if strings.HasPrefix(path, "internal/views/components/") {
			name = strings.TrimPrefix(path, "internal/views/")
		}
		_, err = templ.New(name).Parse(string(content))
		return err
	})
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	routes.RegisterRoutes(mux, templ, appVersion)

	port := fmt.Sprintf(":%s", os.Getenv("PORT"))
	fmt.Printf("http://localhost%s\n", port)
	if err := http.ListenAndServe(port, mux); err != nil {
		panic(err)
	}
}
