package main

import (
	"fmt"
	"net/http"
	"github.com/gorilla/websocket"
	"time"
	"bytes"
	"encoding/json"
)	

func main () {
	default_port := fmt.Sprintf(":%d", 8000)
	fmt.Printf("Listening on port %s\n", default_port)
	
	//TODO: criar view para listar salas
	main_room:= newRoom("Main")
	go main_room.run()

	http.HandleFunc("/", index_view)
	http.HandleFunc("/ws", func (w http.ResponseWriter, r *http.Request) {
		handle_websocket_conn(main_room, w, r)
	})

	http.ListenAndServe(default_port, nil)

}

func index_view (w http.ResponseWriter, r *http.Request) {
	fmt.Println(r.URL.Path)
	p := "." + r.URL.Path
	if p == "./" {
		p = "./public/index.html"
	}
	http.ServeFile(w, r, p)
}

var upgrader = websocket.Upgrader{
	ReadBufferSize: 1024,
	WriteBufferSize: 1024,
}

type Room struct {
	// Room identifier
	name string

	// Registered clients.
	clients map[*Client]bool

	// Inbound messages from the clients.
	broadcast chan []byte

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client
}

func (r *Room) broadcastRoomInfo() {
    count_message := map[string]interface{}{
        "type": "room-info",
		"room_name": r.name,
		"users": len(r.clients),
    }
    
    data, _ := json.Marshal(count_message)
    
    for client := range r.clients {
        select {
        	case client.send <- data:
        }
    }
}

func (r *Room) run() {
	for {
		select {
		case client := <-r.register:
			r.clients[client] = true
			r.broadcastRoomInfo()
		case client := <-r.unregister:
			if _, ok := r.clients[client]; ok {
				delete(r.clients, client)
				close(client.send)
			}
			r.broadcastRoomInfo()
		case message := <-r.broadcast:
			for client := range r.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(r.clients, client)
				}
			}
		}
	}
}

func newRoom(name string) *Room {
	return &Room{
		name: 		name,
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
	}
}

type Client struct {
	//TODO: adicionar username no client para notificar quando um usuario entra
	
	room *Room

	// The websocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	send chan []byte
}

func (c *Client) readPump() {
	defer func(){
		c.room.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(60 * time.Second)); return nil })
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure){
				fmt.Println("error: ", err)
			}
			break
		}
		message = bytes.TrimSpace(bytes.Replace(message, []byte{'\n'}, []byte{' '}, -1))
		fmt.Println("Message Received: ", string(message))
		c.room.broadcast <- message
	}
}

func (c *Client) writePump () {
	ticker := time.NewTicker(((60 * time.Second) * 9) / 10)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)
			n := len(c.send)
			for i := 0; i < n; n++ {
				w.Write(<-c.send)
			}
			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func handle_websocket_conn(room *Room, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	client := &Client{room: room, conn: conn, send: make(chan []byte, 256)}
	client.room.register <- client
	
	go client.writePump()
	go client.readPump()
}