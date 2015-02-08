// inspired from https://github.com/StalkR/goircbot/blob/master/bot/bot.go and
// https://github.com/lukevers/kittens/blob/master/main.go
package main

import (
	"fmt"
	irc "github.com/fluffle/goirc/client"
	"github.com/fluffle/goirc/logging"
	//redis "github.com/vmihailenco/redis"
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
	Config     *irc.Config
	connection *irc.Conn
	reconnect  bool
	quit       chan bool
}

func Create() Bot {
	logging.Debug("creating botter")

	cfg := irc.NewConfig("tobog")
	cfg.SSL = false
	cfg.Server = "nibelheim:6667"
	cfg.NewNick = func(n string) string { return n + "^" }
	cfg.PingFreq = 30 * time.Second
	cfg.Timeout = 120 * time.Second
	connection := irc.Client(cfg)

	bot := &Botter{
		quit:       make(chan bool),
		connection: connection,
		reconnect:  true,
	}

	bot.Config = cfg
	connection.EnableStateTracking()

	connection.HandleFunc(irc.CONNECTED, func(conn *irc.Conn, line *irc.Line) {
		fmt.Printf("Connected to %s", cfg.Server)
		fmt.Printf("joining channels")
		conn.Join("#moo")
	})

	connection.HandleFunc(irc.DISCONNECTED, func(conn *irc.Conn, line *irc.Line) {
		fmt.Printf("Disconncted from %s", cfg.Server)
		fmt.Printf("Reconnecting...")
		bot.quit <- true
	})

	connection.HandleFunc(irc.PRIVMSG, func(conn *irc.Conn, line *irc.Line) {
		fmt.Printf("PRIVMSG : %+v", line.Raw)
	})

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
