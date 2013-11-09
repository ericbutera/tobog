// https://gobyexample.com
// https://github.com/go-nut/gobot/blob/master/irc.go
// http://stackoverflow.com/questions/13342128/simple-golang-irc-bot-keeps-timing-out
// https://github.com/Autumn/irc.go
package main

import (
  // "reflect"
  //"log"
  "fmt"
  "net"
  "bufio"
  "strings"
  "time"
  "regexp"
  "container/list"
  "net/textproto"
  "math/rand"
  "encoding/json"
  redis "github.com/vmihailenco/redis"
  gcfg "code.google.com/p/gcfg"
)

func CreateMessage(raw string) *Message {
  var source, message, to, code string
  var isAction, isCtcp bool = false, false
  netmask := &Netmask{ Origin: "server"}

  if len(raw) > 1 && ":" == raw[0:1] {
    offset := strings.Index(raw, " ")
    source = raw[1:offset]

    // command to parse 001 PRIVMSG NOTICE JOIN QUIT MODE
    rawCommand := raw[offset + 1 : len(raw)]
    // fmt.Printf("parsing [%+v]\n", rawCommand)

    // privmsg - todo: refactor
    cmdOffset := strings.Index(rawCommand, " ")
    code = rawCommand[0:cmdOffset]

    if cmdOffset > 0 && "PRIVMSG" == rawCommand[0:cmdOffset] {
      colon := strings.Index(rawCommand, ":")
      actionChar := "\u0001"

      startToOffset := 8
      endToOffset := colon - 1
      to = rawCommand[startToOffset:endToOffset] //Message.To

      // fmt.Printf("raw [%v]\n", []byte(rawCommand))
      if rawCommand[colon+1:colon+2] == actionChar {
        if len(rawCommand) > colon+9 && rawCommand[colon+2:colon+8] == "ACTION" {
          // action
          message = rawCommand[colon+9: len(rawCommand)-1]
          isAction = true
        } else {
          // ctcp
          message = rawCommand[colon+2: len(rawCommand)-1]
          isCtcp = true
        }
      } else {
        message = rawCommand[colon+1: len(rawCommand)]
      }
    }

    // netmask - refactor
    reg, err := regexp.Compile(`([^!@]+)!([^@]+)@(.*)`) // put this somewhere else
    if err == nil {
      match := reg.FindStringSubmatch(source)
      if match != nil {
        netmask.Nick = match[1]
        netmask.Ident = match[2]
        netmask.Host = match[3]
        netmask.Origin = "user"
        // fmt.Printf("regex match %v netmask %v \n", match, netmask)
      }
    }
  } else {
    source = "local"
    message = raw
  }

  return &Message{
    Netmask: netmask,
    Source: source,
    Message: message,
    To: to,
    Code:code,
    IsAction: isAction,
    IsCtcp: isCtcp,
    Raw: raw}
}

func MessageChan() chan *Message {
  return make(chan *Message)
}

type Bot struct {
  server string
  port string
  nick string
  user string
  channel string

  plugins *list.List
  chans *list.List
  send chan *SendMessage
  recv chan *ServerResponse
  broadcast chan *Message

  err chan error

  conn net.Conn
  reader *textproto.Reader
  writer *textproto.Writer

  ignoredCodes []string
}

func NewBot(cfg Config) *Bot {
  return &Bot{
    server: cfg.General.Server, // "localhost",
    port: cfg.General.Port,  //"6667",
    nick: cfg.General.Nick, // "moo",
    channel: cfg.General.Channel, // "#moo",
    user: cfg.General.User, // "moo",
    plugins: list.New(),
    chans: list.New(),
    err: make(chan error),
    conn: nil,
    ignoredCodes: []string { "002", "003", "005", "250", "251", "254", "255", "265", "266", "372", "375", "376" }}
}

func (bot *Bot) Read(reconnect <-chan bool) {
  // reads messages from server
  for {
    select {
      case <- reconnect:
        fmt.Printf("bot.Read got reconnect message, exit [%v]\n", reconnect)
        return
      default:
        line, err := bot.reader.ReadLine()
        if err != nil {
          fmt.Printf("bot.Read error, exiting %v\n", err)
          bot.err <- err
          return
        } else {
          if bot.recv != nil {
            if len(line) > 4 && line[0:4] == "PING" {
              bot.Cmd("PONG :%s", line[6:len(line)])
            }
            // fmt.Printf("bot.Read val of bot.recv: [%v]\n", bot.recv)
            bot.recv <- &ServerResponse{msg: line}
          }
        }
    }
  }
}

func (bot *Bot) Write(reconnect <-chan bool) {
  fmt.Printf("bot.Write\n")
  // sends messages to server
  for {
    select {
      case <- reconnect:
        fmt.Printf("bot.Write got reconnect message, exit\n")
        return
      case msg, ok := <-bot.send:
        if !ok {
          fmt.Printf("bot.Write not okay, exit msg[%v] ok[%v]\n", msg, ok)
          return
        }
        if (msg == nil) {
          fmt.Printf("bot.Write encountered msg error [%v]\n", msg)
        } else {
          fmt.Printf("bot.Write calling [%+v] bot.Cmd(%s %s %s)", msg, msg.command, msg.target, msg.message)
          bot.Cmd("%s %s :%s", msg.command, msg.target, msg.message)
        }
    }
  }
}

func (bot *Bot) BroadcastMessage(reconnect <-chan bool) {
  fmt.Printf("bot.BroadcastMessage\n")
  for {
    select {
      case <- reconnect:
        fmt.Printf("bot.BroadcastMessage got reconnect message, exit\n")
        return
      case msg, ok := <-bot.broadcast:
        if !ok {
          fmt.Printf("BroadcastMessage not okay, exiting [%v]\n", ok)
          return
        }
        for i:= bot.chans.Front(); i != nil; i = i.Next() {
          ch := i.Value.(chan *Message)
          ch <- msg
        }
    }
  }
}

func (bot *Bot) ExecutePlugins(reconnect <-chan bool) {
  fmt.Printf("bot.ExecutePlugins\n")
  // todo: move this out of connect.  figure out a way to pass in a list of fns so they can get relaunched each conn refresh
  bot.Plugin(registerPlugin)
  bot.Plugin(nickInUsePlugin)
  bot.Plugin(joinChannelPlugin)
  bot.Plugin(loggerPlugin)
  bot.Plugin(redisFromIrcPlugin)
  go redisToIrcPlugin(bot, reconnect) // bot.Plugin(redisToIrcPlugin)

  for i := bot.plugins.Front(); i != nil; i = i.Next() {
    ch := MessageChan()
    bot.chans.PushBack(ch)
    fn := i.Value.(func(bot *Bot, ch <-chan *Message, reconnect <-chan bool))
    fmt.Printf("executing plugin %v\n", fn)
    go fn(bot, ch, reconnect)
  }
}

func (bot *Bot) Quit() {
  fmt.Printf("bot.Quit\n")
  bot.Cmd("QUIT")
}

func (bot *Bot) Reconnect(reconnect chan bool) error {
  fmt.Printf("bot.Reconnect running rec:[%v] \n", reconnect)

  // i think i need to close these????
  bot.reader = nil
  bot.writer = nil

  bot.plugins = list.New()
  bot.chans = list.New()
  for i:= bot.chans.Front(); i != nil; i = i.Next() {
    ch := i.Value.(chan *Message)
    close(ch)
  }

  fmt.Printf("closing bot.send\n")
  close(bot.send)
  fmt.Printf("closing bot.recv\n")
  close(bot.recv)
  fmt.Printf("closing bot.broadcast\n")
  close(bot.broadcast)
  fmt.Printf("closing reconnect\n")
  close(reconnect)

  return bot.Connect()
}

func (bot *Bot) Connect() error {
  fmt.Printf("bot.Connect running\n")

  con, err := net.Dial("tcp", bot.server +":"+ bot.port)
  if err != nil {
    return err
  }
  defer con.Close()
  bot.conn = con

  fmt.Printf("creating new channels\n")
  reconnect := make(chan bool)
  bot.send = make(chan *SendMessage)
  bot.recv = make(chan *ServerResponse)
  bot.broadcast = make(chan *Message)

  bot.reader = textproto.NewReader(bufio.NewReader(con))
  bot.writer = textproto.NewWriter(bufio.NewWriter(con))
  fmt.Printf("bot reader[%v] writer[%v] \n", bot.reader, bot.writer)

  go bot.Write(reconnect)
  go bot.Read(reconnect)
  bot.ExecutePlugins(reconnect) // reconnect??
  go bot.BroadcastMessage(reconnect)

  for {
    select {
      case resp, ok := <-bot.recv:
        if !ok {
          // causes process to exit
          return err
          fmt.Printf("main loop response not ok! \n")
          //reconnect <- true
          //bot.Reconnect(reconnect)
        }
        // fmt.Printf("raw[%+v] ok[%+v]\n", resp, ok)
        if ok && resp != nil {
          msg := CreateMessage(resp.msg)
          if bot.AllowedCode(msg.Code) {
            // fmt.Printf("[%v][%v] resp[%+v]\n", bot.conn, bot.reader, resp)
            bot.broadcast <- msg
          }
        }
      case <-time.After(time.Second * 900):
        fmt.Printf("no read within 900 seconds, reconnecting\n")
        reconnect <- true
        // bot.Reconnect(reconnect)
      case err := (<-bot.err): 
        fmt.Printf("bot error detected %v \n", err)
        reconnect <- true
        bot.Reconnect(reconnect)
    }
  }
  return nil
}

func (bot *Bot) AllowedCode(code string) bool {
  allow := true
  for _, ignored := range bot.ignoredCodes {
    if code == ignored {
      allow = false
    }
  }
  return allow
}

// registers a plugin
func (bot *Bot) Plugin(callback func(bot *Bot, ch <-chan *Message, reconnect <-chan bool)) {
  bot.plugins.PushBack(callback)
}

func (bot *Bot) Cmd(strfmt string, args...interface{}) {
  fmt.Printf("write: [" + strfmt + "]\n", args...)
  if bot.conn == nil {
    fmt.Printf("bot.Cmd attempting to write with no connect\n")
    return
  } else {
    err := bot.writer.PrintfLine(strfmt, args...);
    if err != nil {
      fmt.Printf("error with bot printfline\n")
      bot.err <- err
    }
  }
}

type FromRedis struct {
  Type string
  To string
  Message string
  Command string
}
func (bot *Bot) HandleRedisCommand(msg *redis.Message) {
  // "publish TOIRC "+ JSON.stringify({Type:"raw", To:"", Message:"", Command:"NICK yomama"}).replace(/"/gi, '\\"');
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
}

type Message struct {
  Netmask *Netmask
  Source string // raw netmask or server
  Code string // NOTICE PRIVMSG JOIN QUIT 001
  To string // channel/nick
  Message string
  IsAction bool
  IsCtcp bool
  Raw string
}

type SendMessage struct {
  command string
  target string
  message string
}

type ServerResponse struct {
  err error
  msg string
}

type Netmask struct {
  Nick string
  Ident string
  Host string
  Origin string // todo: const
}

// handles registration on first notice 
func registerPlugin(bot *Bot, ch <-chan *Message, reconnect <-chan bool) {
  var firstNotice = false
  for {
    select {
      case <- reconnect:
        return
      case msg := <-ch:
        if msg != nil && !firstNotice && strings.Contains(msg.Raw, "NOTICE AUTH") {
          bot.Cmd("USER %s 8 * :%s", bot.nick, bot.nick)
          bot.Cmd("NICK %s", bot.nick)
          firstNotice = true
          // return // figure out how to exit this routine
        }
    }
  }
}

func joinChannelPlugin(bot *Bot, ch <-chan *Message, reconnect <-chan bool) {
  for {
    select {
      case <- reconnect:
        return
      case msg := <-ch:
        if msg != nil && msg.Code == "004" {
          // on 004, join channels
          bot.Cmd("JOIN %s", bot.channel) // todo wait for code
          // return // figure out how to exit this routine
        }
    }
  }
}

// todo: set a timer to try and regain original nick
func nickInUsePlugin(bot *Bot, ch <-chan *Message, reconnect <-chan bool) {
  for {
    select {
      case <- reconnect:
        return
      case msg := <-ch:
        if msg != nil && 
          (msg.Code == "432" ||  msg.Code == "433") {
          rand.Seed(time.Now().Unix())
          randomness := rand.Intn(1000 - 1) + 1
          bot.Cmd("NICK %s%d", bot.nick, randomness) // todo nick in use
        }
    }
  }
}

func loggerPlugin(bot *Bot, ch <-chan *Message, reconnect <-chan bool) {
  fmt.Printf("plugin Logger running\n")
  for {
    select {
      case <- reconnect:
        fmt.Printf("loggerPlugin got reconnect message, exit\n")
        return
      case msg := <-ch:
        if msg != nil && len(msg.Message) > 0 {
          //fmt.Printf("[%v][%v]read: src[%s] msg[%s]\n", bot.conn, bot.reader, msg.Source, msg.Message)
          fmt.Printf("read: src[%s] to[%s] msg[%s]\n", msg.Source, msg.To, msg.Message)
        }
    }
  }
}

func redisFromIrcPlugin(bot *Bot, ch <-chan *Message, reconnect <-chan bool) {
  fmt.Printf("plugin Redis from irc running\n")
  password := ""  // no password set
  db := int64(-1) // use default DB
  client := redis.NewTCPClient("localhost:6379", password, db)

  for {
    select {
      case <- reconnect:
        fmt.Printf("redisFromIrcPlugin got reconnect message, exit\n")
        return
      case msg := <-ch:
        fmt.Printf("redis FROMIRC [%+v]\n", msg)
        if len(msg.Raw) > 4 && "PING" != msg.Raw[0:4] {
          jstr, err := json.Marshal(msg)
          if err == nil {
            client.Publish("FROMIRC", string(jstr))
          }
        }
    }
  }
}

func redisToIrcPlugin(bot *Bot, reconnect <-chan bool) {
  fmt.Printf("plugin Redis to irc running\n")
  password := ""  // no password set
  db := int64(-1) // use default DB
  client := redis.NewTCPClient("localhost:6379", password, db)

  pubsub, err := client.PubSubClient()
  defer pubsub.Close()
  toirc, err := pubsub.Subscribe("TOIRC")
  _ = err

  for {
    select {
      case <- reconnect:
        fmt.Printf("redisToIrcPlugin got reconnect message, exit\n")
        return
      case msg := <-toirc:
        fmt.Printf("TOIRC [%+v]\n", msg)
        bot.HandleRedisCommand(msg)
    }
  }
}

type Config struct {
  General struct {
    Server string
    Port string
    Nick string
    Channel string
    User string
  }
}

func main() {
  var cfg Config
  err := gcfg.ReadFileInto(&cfg, "config.gcfg")
  if nil != err {
    fmt.Printf("Invalid config %+v\n", err)
    return
  }

  bot := NewBot(cfg)
  bot.server = "localhost"
  bot.Connect()
}

