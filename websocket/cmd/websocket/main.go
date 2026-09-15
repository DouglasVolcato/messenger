package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	utils "github.com/douglasvolcato/messager-architecture-challenge/pkg"

	"github.com/douglasvolcato/messager-architecture-challenge/cache"
	"github.com/gorilla/websocket"
)

const (
	writeWait  = 5 * time.Second
	pongWait   = 30 * time.Second
	pingPeriod = (pongWait * 9) / 10
	sendBuffer = 64
	redisTTL   = 45 * time.Second
)

type Client struct {
	userID       string
	connectionID string
	serverID     string
	conn         *websocket.Conn
	send         chan []byte
}

func main() {
	serverID, err := utils.GenerateUUID()
	if err != nil {
		panic(fmt.Sprintf("Error generating server ID: %v", err))
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}

	err = cache.InitRedisClient()
	if err != nil {
		log.Fatalf("Failed to initialize Redis client: %v", err)
	}
	defer cache.CloseRedisClient()

	http.HandleFunc("/ws/notifications", func(w http.ResponseWriter, r *http.Request) {
		user, err := utils.GetUserFromCookie(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("Upgrade error: %v", err)
			return
		}

		connID, _ := utils.GenerateUUID()

		client := &Client{
			userID:       user.ID,
			connectionID: connID,
			serverID:     serverID,
			conn:         conn,
			send:         make(chan []byte, sendBuffer),
		}

		ctx := context.Background()
		redisKey := fmt.Sprintf("user:sessions:%s", user.ID)

		expiresAt := time.Now().Add(redisTTL).Unix()
		fieldValue := fmt.Sprintf("%s:%d", serverID, expiresAt)

		if err := cache.RDB.HSet(ctx, redisKey, connID, fieldValue).Err(); err != nil {
			log.Printf("Failed to save session in Redis: %v", err)
			conn.Close()
			return
		}

		cache.RDB.Expire(ctx, redisKey, 7*24*time.Hour)

		go client.writePump()
		go client.readPump()
	})

	log.Println("Multi-Session WebSocket Server starting on :8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	ctx := context.Background()
	redisKey := fmt.Sprintf("user:sessions:%s", c.userID)

	defer func() {
		ticker.Stop()
		c.conn.Close()
		cache.RDB.HDel(ctx, redisKey, c.connectionID)
	}()

	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

			expiresAt := time.Now().Add(redisTTL).Unix()
			fieldValue := fmt.Sprintf("%s:%d", c.serverID, expiresAt)
			cache.RDB.HSet(ctx, redisKey, c.connectionID, fieldValue)
		}
	}
}

func (c *Client) readPump() {
	defer func() {
		c.conn.Close()
		cache.RDB.HDel(context.Background(), fmt.Sprintf("user:sessions:%s", c.userID), c.connectionID)
	}()

	c.conn.SetReadLimit(128)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (c *Client) SendNotification(notification []byte) {
	select {
	case c.send <- notification:
	default:
		log.Printf("[SLOW CLIENT] Dropping connection for user %s", c.userID)
		close(c.send)
		c.conn.Close()
	}
}
