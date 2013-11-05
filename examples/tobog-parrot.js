var redis = require("redis"),
  fromirc = redis.createClient(),
  toirc = redis.createClient();

fromirc.on("message", function (channel, message) {
  // {"Netmask":{"Nick":"eric_","Ident":"eric","Host":"i.love.debian.org","Origin":"user"},"Source":"eric_!eric@i.love.debian.org","Message":"PRIVMSG #moo :wat"}
  console.log("message ch [%j] msg[%j]", channel, message);
  try {
    var m = JSON.parse(message);
    var res = toirc.publish("TOIRC", JSON.stringify({
      Type:"privmsg",
      To:"#moo",
      Message: m.Netmask.Nick +" said: "+ m.Message,
      Command:""
    }));
    console.log("published %j", res);
  } catch (e) {
    console.log("err %j", e);
  }
});
fromirc.subscribe("FROMIRC");

