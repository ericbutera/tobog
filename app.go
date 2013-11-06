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
)

func MessageChan() chan *Message {
  return make(chan *Message)
}

type Bot struct {
  server string
  port string
  nick string
  user string
  channel string
  pass string

  plugins *list.List
  chans *list.List
  send chan *SendMessage
  recv chan *ServerResponse
  bot chan *Message

  err chan error

  conn net.Conn
  reader *textproto.Reader
  writer *textproto.Writer
}

func NewBot() *Bot {
  return &Bot{
    server: "localhost",
    port: "6667",
    nick: "moo",
    channel: "#moo",
    pass: "",
    user: "moo",
    plugins: list.New(),
    chans: list.New(),
    err: make(chan error),
    conn: nil}
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
          if len(line) > 4 && line[0:4] == "PING" {
            bot.Cmd("PONG :%s", line[6:len(line)])
          } else if bot.recv != nil {
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

func (bot *Bot) BroadcastMessage (reconnect <-chan bool) {
  fmt.Printf("bot.BroadcastMessage\n")
  for {
    select {
      case <- reconnect:
        fmt.Printf("bot.BroadcastMessage got reconnect message, exit\n")
        return
      case msg, ok := <-bot.bot:
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

func (bot *Bot) ExecutePlugins (reconnect <-chan bool) {
  fmt.Printf("bot.ExecutePlugins\n")
  // todo: move this out of connect.  figure out a way to pass in a list of fns so they can get relaunched each conn refresh
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

func (bot *Bot) Register() {
  rand.Seed(time.Now().Unix())
  randomness := rand.Intn(1000 - 1) + 1
  bot.Cmd("USER %s 8 * :%s", bot.nick, bot.nick)
  bot.Cmd("NICK %s%d", bot.nick, randomness) // todo nick in use
  bot.Cmd("JOIN %s", bot.channel) // todo wait for code
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

  close(bot.send)
  close(bot.recv)
  close(bot.bot)
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

  reconnect := make(chan bool)
  bot.send = make(chan *SendMessage)
  bot.recv = make(chan *ServerResponse)
  bot.bot = make(chan *Message)

  bot.reader = textproto.NewReader(bufio.NewReader(con))
  bot.writer = textproto.NewWriter(bufio.NewWriter(con))
  fmt.Printf("bot reader[%v] writer[%v] \n", bot.reader, bot.writer)

  go bot.Write(reconnect)
  go bot.Read(reconnect)
  bot.ExecutePlugins(reconnect) // reconnect??
  go bot.BroadcastMessage(reconnect)
  bot.Register()

  for {
    select {
      case resp := <-bot.recv:
        bot.bot <-CreateMessage(resp.msg)
      case <-time.After(time.Second * 900):
        fmt.Printf("no read within 900 seconds, reconnecting\n")
        reconnect <- true
        bot.Reconnect(reconnect)
      case err := (<-bot.err): 
        fmt.Printf("bot error detected %v \n", err)
        reconnect <- true
        bot.Reconnect(reconnect)
    }
  }
  return nil
}

func CreateMessage(raw string) *Message {
  var source, message string
  netmask := &Netmask{ Origin: "server"}

  if len(raw) > 1 && ":" == raw[0:1] {
    offset := strings.Index(raw, " ")
    source = raw[1:offset]
    message = raw[offset + 1 : len(raw)]

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

    /*
    "PRIVMSG #moo :hey"                    - hey
    "PRIVMSG #moo :\u0001ACTION hey\u0001" - /me hey
    "PRIVMSG moo603 :\u0001VERSION\u0001"  - /ctcp moo603 VERSION
    "PRIVMSG #moo :\u0001VERSION\u0001"    - /ctcp #moo VERSION
    */
    if len(raw) > 7 && "PRIVMSG" == raw[0:6] {
      // to = offset 8 thru first space
      // next = first char after : to EOL
    }
  } else {
    source = "local"
    message = raw
  }

  return &Message{
    Netmask: netmask,
    Source: source,
    Message: message,
    To: "",
    Type: "msg", // ctcp, msg, channel
    Action: false}
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
  // {Type:"privmsg",To:"#moo",Message:"hello world!",Command:"smile"}
  fmt.Printf("bot handle redis command %+v\n", msg.Message)
  var cmd FromRedis
  err := json.Unmarshal([]byte(msg.Message), &cmd)
  fmt.Printf("cmd %+v\n", cmd)
  if err == nil {
    switch (cmd.Type) {
      case "privmsg":
        bot.Cmd("PRIVMSG %s :%s", cmd.To, cmd.Message)
      case "action":
        bot.Cmd("PRIVMSG %s :%s", cmd.To, cmd.Message)
      case "raw":
        bot.Cmd("%s", cmd.Command)
    }
  } else {
    fmt.Printf("error %+v\n", err)
  }
}

type Message struct {
  Netmask *Netmask
  Source string
  Message string
  To string
  Type string // ctcp, msg, channel
  Action bool
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

func loggerPlugin(bot *Bot, ch <-chan *Message, reconnect <-chan bool) {
  fmt.Printf("plugin Logger running\n")
  for {
    select {
      case <- reconnect:
        fmt.Printf("loggerPlugin got reconnect message, exit\n")
        return
      case msg := <-ch:
        // fmt.Printf("[%v][%v]read: src[%s] msg[%s]\n", bot.conn, bot.reader, msg.Source, msg.Message)
        if (msg != nil) {
          code := msg.Message[0:3]
          if ("372" != code &&
            "002" != code &&
            "003" != code &&
            "004" != code &&
            "005" != code &&
            "250" != code &&
            "251" != code &&
            "254" != code &&
            "255" != code &&
            "265" != code &&
            "266" != code &&
            "375" != code &&
            "376" != code) {
            fmt.Printf("[%v][%v]read: src[%s] msg[%s]\n", bot.conn, bot.reader, msg.Source, msg.Message)
          }
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
        jstr, _ := json.Marshal(msg)
        // fmt.Printf("FROMIRC [%s]\n", string(jstr))
        client.Publish("FROMIRC", string(jstr))
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


func main() {
  bot := NewBot()
  bot.server = "localhost"
  bot.Connect()
}

