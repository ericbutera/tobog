// inspired from https://github.com/StalkR/goircbot/blob/master/bot/bot.go and
package main

import (
	//"encoding/json"
	"fmt"
	irc "github.com/fluffle/goirc/client"
	"github.com/fluffle/goirc/logging"
	redis "gopkg.in/redis.v2"
	"time"
)

func main() {
	var logger logging.Logger = stdoutLogger{}
	logging.SetLogger(logger)
	logging.Debug("Launching botter")
	botter := Create()
	botter.Run()
}

type stdoutLogger struct{}

func (nl stdoutLogger) Debug(f string, a ...interface{}) { fmt.Printf("DEBUG %+v\n", a) }
func (nl stdoutLogger) Info(f string, a ...interface{})  { fmt.Printf("INFO %+v", a) }
func (nl stdoutLogger) Warn(f string, a ...interface{})  { fmt.Printf("WARN %+v\n", a) }
func (nl stdoutLogger) Error(f string, a ...interface{}) { fmt.Printf("ERROR %+v\n", a) }

type Bot interface {
	Connection() *irc.Conn
	Run()
	Quit(message string)
	Join(channel string)
	Privmsg(t, message string)
	Connected() bool
}

type Botter struct {
	Config      *irc.Config
	connection  *irc.Conn
	reconnect   bool
	quit        chan bool
	redisClient *redis.Client
	redisPubSub *redis.PubSub
}

func Create() Bot {
	logging.Debug("creating botter")

	cfg := irc.NewConfig("tobog")
	cfg.SSL = false
	cfg.Server = "localhost:6667"
	//cfg.NewNick = func(n string) string { return n + "^" }
	cfg.PingFreq = 30 * time.Second
	cfg.Timeout = 120 * time.Second
	connection := irc.Client(cfg)

	bot := &Botter{
		quit:       make(chan bool),
		connection: connection,
		reconnect:  true,
		Config:     cfg,
	}

	connection.EnableStateTracking()

	connection.HandleFunc(irc.CONNECTED, func(conn *irc.Conn, line *irc.Line) {
		fmt.Printf("Connected to %s", cfg.Server)
		fmt.Printf("joining channels")
		conn.Join("#moo")
		bot.FromIrc("connected to server")
	})

	connection.HandleFunc(irc.DISCONNECTED, func(conn *irc.Conn, line *irc.Line) {
		fmt.Printf("Disconncted from %s", cfg.Server)
		fmt.Printf("Reconnecting...")
		bot.FromIrc("disconnected from server")
		bot.quit <- true
	})

	connection.HandleFunc(irc.PRIVMSG, func(conn *irc.Conn, line *irc.Line) {
		fmt.Printf("PRIVMSG : %+v", line.Raw)
		bot.FromIrc(fmt.Sprintf("bot got message %v", line.Raw))
	})

	bot.Redis()

	return bot
}

func (bot *Botter) Run() {
	for bot.reconnect {
		if err := bot.Connection().Connect(); err != nil {
			fmt.Printf("Error connecting: ", err)
			time.Sleep(time.Minute)
			continue
		}

		<-bot.quit // wait for disconnect
	}
}

func (bot *Botter) Quit(message string) {
	bot.reconnect = false
	bot.connection.Quit(message)
}

func (bot *Botter) Connection() *irc.Conn     { return bot.connection }
func (bot *Botter) Join(channel string)       { bot.Connection().Join(channel) }
func (bot *Botter) Privmsg(t, message string) { bot.Connection().Privmsg(t, message) }
func (bot *Botter) Connected() bool           { return bot.Connection().Connected() }

func (bot *Botter) Redis() {
	// todo: connection error handling.  unable to connect/reconnect
	var client *redis.Client
	client = redis.NewTCPClient(&redis.Options{
		Addr: "localhost:6379",
	})
	client.Publish("FROMIRC", "meeeeh moo")

	bot.redisClient = client
	go bot.RedisPubSub(client)
}

func (bot *Botter) RedisPubSub(client *redis.Client) {
	pubsub := client.PubSub()
	defer pubsub.Close()

	err := pubsub.Subscribe("TOIRC")
	if err != nil {
		panic("oh hamburgers")
	}

	for {
		msg, err := pubsub.Receive()
		fmt.Printf("message from redis %+v err %+v", msg, err)
		if err == nil {
			switch msg.(type) {
			case *redis.Message:
				bot.ToIrc(msg.(*redis.Message).Payload)
			}
		}
	}
}

func (bot *Botter) FromIrc(message string) {
	fmt.Printf("FromIrc got a message %v", message)
	bot.redisClient.Publish("FROMIRC", message)
	/*
	   fmt.Printf("redis FROMIRC [%+v]\n", msg)
	   if len(msg.Raw) > 4 && "PING" != msg.Raw[0:4] {
	     fmt.Printf("redis FROMIRC message not ping\n")
	     jstr, err := json.Marshal(msg)
	     fmt.Printf("redis FROMIRC jstr [%+v] err [%+v]\n", jstr, err)
	     if err == nil {
	       client.Publish("FROMIRC", string(jstr))
	     }
	   }
	*/
}

func (bot *Botter) ToIrc(message string) {
	fmt.Printf("TOIRC got a message, so special! %+v", message)
	bot.Privmsg("#moo", message)
	/*
	  // "publish TOIRC "+ JSON.stringify({Type:"privmsg", To:"#moo", Message:"a message", Command:""}).replace(/"/gi, '\\"');
	  var cmd FromRedis
	  err := json.Unmarshal([]byte(msg.Message), &cmd)
	  if err == nil {
	    fmt.Printf("cmd %+v\n", cmd)
	    switch (cmd.Type) {
	      case "privmsg":
	        bot.Cmd("PRIVMSG %s :%s", cmd.To, cmd.Message)
	      case "action":
	        bot.Cmd("PRIVMSG %s :\u0001ACTION%s\u0001", cmd.To, cmd.Message)
	      case "ctcp":
	        bot.Cmd("PRIVMSG %s :\u0001%s\u0001", cmd.To, cmd.Message)
	      case "raw":
	        bot.Cmd("%s", cmd.Command)
	    }
	  }
	*/
}
