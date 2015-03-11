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

type IniConfig struct {
	channel string
	nick    string
	server  string
	port    string
	fromirc string
	toirc   string
}

func main() {
	// fix this horrible nested error checking
	if len(os.Args) != 2 {
		panic("Provide an ini file as the second run parameter")
	} else {
		iniconfig, err := ini.LoadFile(os.Args[1])
		if err != nil {
			panic("Unable to load config file")
		} else {
			ininick, ok := iniconfig.Get("bot", "nick")
			if !ok {
				panic("Invalid config")
			} else {
				inichannel, _ := iniconfig.Get("bot", "channel")
				iniserver, _ := iniconfig.Get("bot", "server")
				iniport, _ := iniconfig.Get("bot", "port")
				inifromirc, _ := iniconfig.Get("bot", "fromirc")
				initoirc, _ := iniconfig.Get("bot", "toirc")
				cfg := &IniConfig{
					channel: inichannel,
					server:  iniserver,
					port:    iniport,
					nick:    ininick,
					toirc:   initoirc,
					fromirc: inifromirc,
				}
				fmt.Printf("cfg %+v\n", cfg)
				var logger logging.Logger = stdoutLogger{}
				logging.SetLogger(logger)
				logging.Debug("Launching botter")
				botter := Create(cfg)
				botter.Run()
			}
		}
	}

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
	iniconfig   *IniConfig
	Config      *irc.Config
	connection  *irc.Conn
	reconnect   bool
	quit        chan bool
	redisClient *redis.Client
	redisPubSub *redis.PubSub
}

func Create(iniconfig *IniConfig) Bot {
	logging.Debug("creating botter")

	cfg := irc.NewConfig("tobog")
	cfg.SSL = false
	fmt.Printf("create got iniconfig %+v\n", iniconfig)
	cfg.Server = fmt.Sprintf("%s:%s", iniconfig.server, iniconfig.port) //"localhost:6668"
	// cfg.NewNick = func(n string) string { return n + "^" }
	cfg.PingFreq = 30 * time.Second
	cfg.Timeout = 120 * time.Second
	connection := irc.Client(cfg)

	bot := &Botter{
		iniconfig:  iniconfig,
		quit:       make(chan bool),
		connection: connection,
		reconnect:  true,
		Config:     cfg,
	}

	connection.EnableStateTracking()

	connection.HandleFunc(irc.CONNECTED, func(conn *irc.Conn, line *irc.Line) {
		fmt.Printf("Connected to %s\n", cfg.Server)
		fmt.Printf("joining channels\n")
		conn.Join(iniconfig.channel) // todo figure out multi joins
	})

	connection.HandleFunc(irc.DISCONNECTED, func(conn *irc.Conn, line *irc.Line) {
		fmt.Printf("Disconncted from %s\n", cfg.Server)
		fmt.Printf("Reconnecting...\n")
		bot.quit <- true
	})

	connection.HandleFunc(irc.PRIVMSG, func(conn *irc.Conn, line *irc.Line) {
		fmt.Printf("PRIVMSG : %+v\n", line.Raw)
		m := CreateRedisLine(line)
		bot.FromIrc(m)
	})

	bot.Redis()

	return bot
}

func (bot *Botter) Run() {
	for bot.reconnect {
		if err := bot.Connection().Connect(); err != nil {
			fmt.Printf("Error connecting: %v\n", err)
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
		Addr: "localhost:6379", //todo put in ini file
	})
	fmt.Printf("from irc name %+v", bot.iniconfig.fromirc)
	//client.Publish(bot.iniconfig.fromirc, "meeeeh moo")

	bot.redisClient = client
	go bot.RedisPubSub(client)
}

func (bot *Botter) RedisPubSub(client *redis.Client) {
	pubsub := client.PubSub()
	defer pubsub.Close()

	err := pubsub.Subscribe(bot.iniconfig.toirc)
	if err != nil {
		panic("oh hamburgers")
	}

	for {
		msg, err := pubsub.Receive()
		fmt.Printf("message from redis %+v err %+v\n", msg, err)
		if err == nil {
			switch msg.(type) {
			case *redis.Message:
				bot.ToIrc(msg.(*redis.Message).Payload)
			}
		}
	}
}

func (bot *Botter) FromIrc(line *ToRedisLine /*message string*/) {
	fmt.Printf("FromIrc got a message %v\n", line)
	jstr, err := json.Marshal(line)
	if err == nil {
		bot.redisClient.Publish(bot.iniconfig.fromirc, string(jstr))
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
	var fromRedisLine FromRedisLine
	err := json.Unmarshal([]byte(message), &fromRedisLine)
	if err != nil {
		fmt.Printf("error %+v\n", err)
	} else {
		fmt.Printf("fromRedisLine: %+v\n", fromRedisLine)
		switch fromRedisLine.Type {
		case PRIVMSG:
			// maybe make privmsg struct?
			fmt.Printf("privmsg %+v\n", fromRedisLine)
			bot.Privmsg(fromRedisLine.To, fromRedisLine.Message)
		case ACTION:
			fmt.Printf("action %+v\n", fromRedisLine)
		case CTCP:
			fmt.Printf("ctcp %+v\n", fromRedisLine)
		case RAW:
			fmt.Printf("raw %+v\n", fromRedisLine)
		}
	}
}

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
