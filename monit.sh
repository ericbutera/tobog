#!/bin/bash
DIR="$( cd "$( dirname "$0" )" && pwd )"

 case $1 in
    start)
       echo $$ > /var/run/tobog.pid;
       exec 2>&1 $DIR/app $DIR/config.gcfg 1>/tmp/tobog.out
       ;;
     stop)
       kill `cat /var/run/tobog.pid` ;;
     *)
       echo "usage: monit.sh {start|stop}" ;;
 esac
 exit 0

