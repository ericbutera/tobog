package main

import (
  // "reflect"
  //"log"
  "fmt"
  "net"
  "bufio"
  "strings"
  //"time"
  "container/list"
  "net/textproto"
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

  readsync, writesync chan bool
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
    /*send: make(chan *SendMessage),
    recv: make(chan *ServerResponse),
    bot: make(chan *Message),*/
    conn: nil}
}

func (bot *Bot) Read() {
  // reads messages from server
  for {
    line, err := bot.reader.ReadLine()
    if err != nil {
      fmt.Println("bot.Read error ", err)
      bot.err <- err
    }
    if (line[0:4] == "PING") {
      bot.Cmd("PONG :%s", line[6:len(line)])
    }
    bot.recv <- &ServerResponse{msg: line}

    select {
      case <-bot.readsync:
        return
      default:
    }
  }
}

func (bot *Bot) BroadcastMessage () {
  for {
    msg := <-bot.bot
    for i:= bot.chans.Front(); i != nil; i = i.Next() {
      ch := i.Value.(chan *Message)
      ch <- msg
    }
  }
}

func (bot *Bot) ExecutePlugins () {
  for i := bot.plugins.Front(); i != nil; i = i.Next() {
    fn := i.Value.(func())
    go fn()
  }
}

func (bot *Bot) Write() {
  fmt.Println("bot.Write")
  // sends messages to server
  for {
    msg := <-bot.send
    bot.Cmd("%s %s :%s", msg.command, msg.target, msg.message)
  }
}

func (bot *Bot) Register() {
  bot.Cmd("USER %s 8 * :%s", bot.nick, bot.nick)
  bot.Cmd("NICK %s", bot.nick) // todo nick in use
  bot.Cmd("JOIN %s", bot.channel) // todo wait for code
}

func (bot *Bot) Quit() {
  fmt.Println("bot.Quit")
  bot.Cmd("QUIT")
}

func (bot *Bot) Reconnect() error {
  fmt.Println("bot.Reconnect running")
  close(bot.send)
  close(bot.recv)

  fmt.Println("setting readsync")
  bot.readsync <- true
  fmt.Println("setting writesync")
  <- bot.writesync

  bot.Quit()

  fmt.Println("reconnect launching connect")
  return bot.Connect()
}

func (bot *Bot) Connect() error {
  fmt.Println("bot.Connect running")
  con, err := net.Dial("tcp", bot.server +":"+ bot.port)
  if err != nil {
    return err
  }
  defer con.Close()
  bot.conn = con

  bot.err = make(chan error)
  bot.send = make(chan *SendMessage)
  bot.recv = make(chan *ServerResponse)
  bot.bot = make(chan *Message)
  bot.readsync = make(chan bool)
  bot.writesync = make(chan bool)

  bot.reader = textproto.NewReader(bufio.NewReader(con))
  bot.writer = textproto.NewWriter(bufio.NewWriter(con))

  go bot.Write()
  go bot.Read()
  bot.ExecutePlugins()
  go bot.BroadcastMessage()
  bot.Register()

  for {
    select {
      case resp := <-bot.recv:
        bot.bot <-CreateMessage(resp.msg)
      /*case <-time.After(time.Second * 6):
        fmt.Println("no read within 600 seconds, reconnecting")
        bot.Reconnect()*/
      case err := (<-bot.err): 
        fmt.Println("bot error detected ", err)
        bot.Reconnect()
    }
    /*
    resp := <-bot.recv
    if resp.err == nil {
      bot.bot <-CreateMessage(resp.msg)
    } else {
      log.Fatal("connection closed")
      break
    }*/
  }
  return nil
}

func CreateMessage(raw string) *Message {
  var netmask, source, message string
  if (":" == raw[0:1]) {
    offset := strings.Index(raw, " ")
    source = raw[1:offset]
    message = raw[offset + 1 : len(raw)]
  } else {
    source = "local-server"
    message = raw
  }
  return &Message{
    Netmask: netmask,
    Source: source,
    Message: message}
}

// registers a plugin
func (bot *Bot) Plugin(ch chan *Message, f func()) {
  bot.chans.PushBack(ch)
  bot.plugins.PushBack(f)
}

func (bot *Bot) Cmd(strfmt string, args...interface{}) {
  fmt.Printf("write: [" + strfmt + "]\n", args...)
  if bot.conn == nil {
    fmt.Println("bot.Cmd attempting to write with no connect")
    return
  }

  err := bot.writer.PrintfLine(strfmt, args...);
  if err != nil {
    bot.err <- err
  }
}

type Message struct {
  /* Server string Nick string User string Host string Command string Target string Message string*/
  Netmask string
  Source string
  Message string
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

func main() {
  bot := NewBot()
  bot.server = "localhost"

  // stdout logger
  logger := MessageChan()
  bot.Plugin(logger, func() {
    for {
      msg := <-logger
      fmt.Printf("read: [%+v]\n", msg)
    }
  })

  password := ""  // no password set
  db := int64(-1) // use default DB
  client := redis.NewTCPClient("localhost:6379", password, db)

  // from irc redis
  fromirc := MessageChan()
  bot.Plugin(fromirc, func() {
    for {
      msg := <-fromirc
      jstr, _ := json.Marshal(msg)
      fmt.Printf("FROMIRC [%s]\n", string(jstr))
      client.Publish("FROMIRC", string(jstr))
    }
  })

  // to irc redis
  go func() {
    pubsub, err := client.PubSubClient()
    defer pubsub.Close()
    toirc, err := pubsub.Subscribe("TOIRC")
    _ = err

    for {
      msg := <-toirc
      fmt.Printf("TOIRC [%s]\n", msg)
    }
  }()

  bot.Connect()
}

