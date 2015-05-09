package main

import (
	"fmt"
	"go-spamc"
)

func main() {

	html := "<html><title>Test</title><body>Hello world. I'm not a Spam, don't kill me spamassassin!</body></html>"

	client := spamc.New("127.0.0.1:783", 10)

	//the 2nd parameter is optional, you can set who (the unix user) do the call
	//looks like client.Report(html, "saintienn")

	reply, err := client.Report(html)

	if err == nil {
		fmt.Println(reply)
	} else {
		fmt.Println(reply, err)
	}

}

/* Example Response
 {
    Code: 0,
    Message: 'EX_OK',
	Vars:{
       isSpam: true,
       spamScore: 6.9,
       baseSpamScore: 5,
       report:[
        {
         "score":   score,
		 "symbol":  x[1],
		 "message": message,
        }
      ]
	}
 }
*/
