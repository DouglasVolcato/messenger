package main

import (
	"fmt"
	"html/template"
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
