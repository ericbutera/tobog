package main

import (
  "fmt"
  "strings"
)

func main() {
  var m = make(map[string]string)
  m["msg"] = "PRIVMSG #chan :lorem ipsum"
  m["action"]= "PRIVMSG #channel :\u0001ACTION lorem ipsum\u0001"
  m["version"] = "PRIVMSG nick :\u0001VERSION\u0001"
  m["chanversion"] = "PRIVMSG #chan :\u0001VERSION\u0001"
  for key, val := range m {
    fmt.Printf("key %v val [%v]\n", key, val)

    colon := strings.Index(val, ":")
    actionChar := "\u0001"
    startNickOffset := 8
    endNickOffset := colon - 1
    nick := val[startNickOffset:endNickOffset]
    fmt.Printf("endNickOffset %d\n", endNickOffset)
    fmt.Printf("nick [%s]\n", nick)

    fmt.Printf("raw [%v]\n", []byte(val))
    if val[colon+1:colon+2] == actionChar {
      fmt.Printf("ctcp or action\n")
      if val[colon+2:colon+8] == "ACTION" {
        msg := val[colon+9: len(val)-1]
        fmt.Printf("action!\n")
        fmt.Printf("msg [%v]\n", msg)
        fmt.Printf("msg [%v]\n", []byte(msg))
      } else {
        msg := val[colon+2: len(val)-1]
        fmt.Printf("action!\n")
        fmt.Printf("msg [%v]\n", msg)
        fmt.Printf("msg [%v]\n", []byte(msg))
      }
    } else {
      fmt.Printf("message!\n")
      msg := val[colon+1: len(val)]
      fmt.Printf("msg [%v]\n", msg)
      fmt.Printf("msg [%v]\n", []byte(msg))
    }

    // 01234567
    // PRIVMSG_TO_:MESSAGE
    fmt.Printf("--------------------------\n")
  }

}

