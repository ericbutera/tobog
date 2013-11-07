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
  m["001"] = "001 moo511 :Welcome to the debian Internet Relay Chat Network moo511"
  m["notice"] = "NOTICE moo511 :*** You are exempt from user limits. congrats."
  m["join"] = "JOIN :#MOO"
  m["quit"] = "QUIT :Quit: leaving"
  m["mode"] = "MODE moo511 :+i"

  for key, val := range m {
    fmt.Printf("key %v val [%v]\n", key, val)

    // figure out command PRIVMSG NOTICE 001 JOIN
    cmdOffset := strings.Index(val, " ")
    fmt.Printf("cmdOffset %d\n", cmdOffset)

    if cmdOffset > 0 {
      cmd := val[0:cmdOffset]
      fmt.Printf("cmd [%v]\n", cmd)

      colon := strings.Index(val, ":")
      actionChar := "\u0001"

      switch cmd {
        case "PRIVMSG":
          startToOffset := 8
          endToOffset := colon - 1
          to := val[startToOffset:endToOffset] //Message.To
          fmt.Printf("endToOffset %d\n", endToOffset)
          fmt.Printf("to [%s]\n", to)

          fmt.Printf("raw [%v]\n", []byte(val))
          if val[colon+1:colon+2] == actionChar {
            fmt.Printf("ctcp or action\n")
            if len(val) > colon+9 && val[colon+2:colon+8] == "ACTION" {
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
        /*case "NOTICE":
          startToOffset := 7
          endToOffset := colon - 1
          to := val[startToOffset:endToOffset] //Message.To
          fmt.Printf("endToOffset %d\n", endToOffset)
          fmt.Printf("to [%s]\n", to)
          msg := val[colon+1: len(val)]
          fmt.Printf("msg [%v]\n", msg)
          fmt.Printf("msg [%v]\n", []byte(msg))
        */
      }

      fmt.Printf("--------------------------\n")
    }
  }

}

