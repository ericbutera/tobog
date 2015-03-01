// inspired from https://github.com/StalkR/goircbot/blob/master/bot/bot.go and
package main

import (
	"encoding/json"
	"fmt"
	irc "github.com/fluffle/goirc/client"
	"github.com/fluffle/goirc/logging"
	"github.com/vaughan0/go-ini"
	redis "gopkg.in/redis.v2"
	"os"
	"time"
)

func main() {
	if len(os.Args) > 1 {
		fmt.Printf("has args %+v", os.Args)
		iniconfig, err := ini.LoadFile(os.Args[1])
		if err == nil {
			fmt.Printf("file %+v", iniconfig)
			//file["section"]
		}
	}
	// name, ok := file.Get("section", "value")
	panic("oh noes")
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
	cfg.Server = "localhost:6668"
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
		conn.Join("#traversecity")
	})

	connection.HandleFunc(irc.DISCONNECTED, func(conn *irc.Conn, line *irc.Line) {
		fmt.Printf("Disconncted from %s", cfg.Server)
		fmt.Printf("Reconnecting...")
		bot.quit <- true
	})

	connection.HandleFunc(irc.PRIVMSG, func(conn *irc.Conn, line *irc.Line) {
		fmt.Printf("PRIVMSG : %+v", line.Raw)
		m := CreateRedisLine(line)
		bot.FromIrc(m)
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

func (bot *Botter) FromIrc(line *ToRedisLine /*message string*/) {
	fmt.Printf("FromIrc got a message %v", line)
	jstr, err := json.Marshal(line)
	if err == nil {
		bot.redisClient.Publish("FROMIRC", string(jstr))
	}
}

type FromRedisLine struct {
	To      string
	Message string // todo: turn into a list of messages
	Type    string
}

const (
	PRIVMSG = "PRIVMSG"
	ACTION  = "ACTION"
	CTCP    = "CTCP"
	RAW     = "RAW"
)

func (bot *Botter) ToIrc(message string) {
	//bot.Privmsg("#moo", message)
	var fromRedisLine FromRedisLine
	err := json.Unmarshal([]byte(message), &fromRedisLine)
	fmt.Printf("error %+v", err)
	fmt.Printf("fromRedisLine: %+v", fromRedisLine)
	if err == nil {
		switch fromRedisLine.Type {
		case PRIVMSG:
			// maybe make privmsg struct?
			fmt.Printf("privmsg %+v", fromRedisLine)
			bot.Privmsg(fromRedisLine.To, fromRedisLine.Message)
		case ACTION:
			fmt.Printf("action %+v", fromRedisLine)
		case CTCP:
			fmt.Printf("ctcp %+v", fromRedisLine)
		case RAW:
			fmt.Printf("raw %+v", fromRedisLine)
		}
	}
}

/*
	Nick, Ident, Host, Src string
	  Cmd, Raw               string
	  Args                   []string
	  Time                   time.Time
*/
type ToRedisLine struct {
	Nick, Ident, Host, Src string
	Cmd, Raw               string
	Args                   []string
	Time                   time.Time
}

func CreateRedisLine(line *irc.Line) *ToRedisLine {
	m := &ToRedisLine{
		Nick:  line.Nick,
		Ident: line.Ident,
		Host:  line.Host,
		Src:   line.Src,
		Cmd:   line.Cmd,
		Raw:   line.Raw,
		Args:  line.Args,
		Time:  line.Time,
	}
	return m
}
