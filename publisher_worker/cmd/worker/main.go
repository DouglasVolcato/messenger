package main

import (
	"os"

	"github.com/douglasvolcato/messager-architecture-challenge/internal/db"
	"github.com/douglasvolcato/messager-architecture-challenge/internal/models"
	"github.com/subosito/gotenv"
)

func main() {
	if err := gotenv.Load(); err != nil {
		if !os.IsNotExist(err) {
			panic(err)
		}
		if parentErr := gotenv.Load("../.env"); parentErr != nil && !os.IsNotExist(parentErr) {
			panic(parentErr)
		}
	}
	if err := db.InitDB(); err != nil {
		panic(err)
	}

	notificationOutbox := &models.UserNotificationOutbox{}
	notifications, total, err := notificationOutbox.GetManyWithLock(db.DB, nil, 1, 10)
	if err != nil {
		panic(err)
	}

	println("Total notifications:", total)
	for _, notification := range notifications {
		println("Notification ID:", notification.ID)
	}
}
