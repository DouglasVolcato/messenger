package main

import (
	"fmt"
	"html/template"
	"net/http"
	"os"

	"github.com/douglasvolcato/messager-architecture-challenge/internal/db"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/routes"
	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"
	"github.com/subosito/gotenv"
)

func main() {
	err := gotenv.Load()
	if err != nil {
		panic(err)
	}

	err = db.InitDB()
	if err != nil {
		panic(err)
	}

	err = db.RunMigrations()
	if err != nil {
		panic(err)
	}

	appVersion, err := utils.GenerateUUID()
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()

	templ, err := template.ParseGlob("internal/views/**/*.html")
	if err != nil {
		panic(err)
	}

	routes.RegisterRoutes(mux, templ, appVersion)

	port := fmt.Sprintf(":%s", os.Getenv("PORT"))
	fmt.Printf("http://localhost%s\n", port)

	http.ListenAndServe(port, mux)
}
