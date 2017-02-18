// This is a small bot that messages someone and replies to everything with a qouted echo
// Additionally you can send messages from commandline.
package main

import (
	"bufio"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
	
	"github.com/o3ma/o3"
)


func main() {
	pass, idpath, abpath, pubnick, pubnickSet, rid, testMsg, createID := parseArgs()
	
	tr, tid, ctx, receiveMsgChan, sendMsgChan := initialise(pass, idpath, abpath, pubnick, pubnickSet, createID)
	
	go receiveLoop(tid, ctx, receiveMsgChan, sendMsgChan)
	
	sendTestMsg(tr, abpath, rid, testMsg, ctx, sendMsgChan)
	
	sendLoop(tr, abpath, ctx, sendMsgChan)
}


func parseArgs() ([]byte, string, string, string, bool, string, string, bool) {
	cmdlnPubnick  := flag.String("nickname",         "parrot", "The nickname for the account (max. 32 chars).")
	cmdlnConfdir  := flag.String("confdir",                "", "Path to the configuration directory.")
	cmdlnPass     := flag.String("pass",           "01234567", "A string which should be at least 8 chars long (else may cause problems).")
	cmdlnHexPass  := flag.String("hexpass",                "", "Like --pass but in hexidecimal. Overrides --pass option. E.g.: 4d7954696e795057")
	cmdlnTestID   := flag.String("testid",                 "", "Send \"testmsg\" to this ID (8 character hex string).")
	cmdlnTestMsg  := flag.String("testmsg",  "Say something!", "Send this message to \"testid\".")
	cmdlnCreateID := flag.Bool(  "createid",            false, "Create a new ID if nessesary without asking for confirmation.")
	flag.Parse()
	flagset := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { flagset[f.Name]=true } )
	
	var (
		pass    = []byte{0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37} // 01234567
		idpath  = "threema.id"
		abpath  = "address.book"
	)
	
	cmdlnPubnickVal := *cmdlnPubnick
	if len(cmdlnPubnickVal) > 32 { cmdlnPubnickVal = cmdlnPubnickVal[0:32] }
	pubnick := cmdlnPubnickVal
	var pubnickSet bool = !!flagset["nickname"]
	
	rid := ""
	ridRegex := regexp.MustCompile("\\A[0-9A-Z]{8}\\z")
	cmdlnTestIDVal := ridRegex.FindString(strings.ToUpper(*cmdlnTestID))
	if cmdlnTestIDVal != "" && len(cmdlnTestIDVal) == 8 { rid = cmdlnTestIDVal }
	
	testMsg := *cmdlnTestMsg
	
	if flagset["hexpass"] {
		hexpass, err := hex.DecodeString(*cmdlnHexPass)
		if err == nil {
			pass = hexpass
		} else {
			fmt.Printf("  Error decoding hexpass!\n")
			os.Exit(1)
		}
	} else {
		pass = []byte(*cmdlnPass)
	}
	if utf8.RuneCountInString(string(pass)) < 8 { fmt.Print("  Warning: Password should have at least 8 characters to avoid problems with original Threema client!\n") }
	
	if (*cmdlnConfdir) != "" {
		idpath = (*cmdlnConfdir) + "/" + idpath
		abpath = (*cmdlnConfdir) + "/" + abpath
	}
	
	createId := *cmdlnCreateID
	
	return pass, idpath, abpath, pubnick, pubnickSet, rid, testMsg, createId
}


func initialise(pass []byte, idpath string, abpath string, pubnick string, pubnickSet bool, createID bool) (o3.ThreemaRest, o3.ThreemaID, o3.SessionContext, <-chan o3.ReceivedMsg, chan<- o3.Message) {
	var (
		tr      o3.ThreemaRest
		tid     o3.ThreemaID
	)
	
	// check whether an id file exists or else create a new one
	if _, err := os.Stat(idpath); err != nil {
		if !createID {
			reader := bufio.NewReader(os.Stdin)
			fmt.Print("  No existing ID found. Create a new one? Enter YES (upper case) or NO: ")
			text, _ := reader.ReadString('\n')
			createID = (text == "YES\n")
		}
		
		if createID {
			var err error
			tid, err = tr.CreateIdentity()
			if err != nil {
				fmt.Println("  CreateIdentity failed!")
				log.Fatal(err)
			}
			fmt.Printf("  Saving ID to %s\n", idpath)
			err = tid.SaveToFile(idpath, pass)
			if err != nil {
				fmt.Println("  Saving ID failed!")
				log.Fatal(err)
			}
		} else {
			os.Exit(1)
		}
	} else {
		fmt.Printf("  Loading ID from %s\n", idpath)
		tid, err = o3.LoadIDFromFile(idpath, pass)
		if err != nil {
			log.Fatal(err)
		}
	}
	fmt.Printf("  Using ID: %s\n", tid.String())
	
	if pubnickSet {
		tid.Nick = o3.NewPubNick(pubnick)
		fmt.Printf("  Setting nickname to: %s\n", pubnick)
	}
	
	ctx := o3.NewSessionContext(tid)
	
	//check if we can load an addressbook
	if _, err := os.Stat(abpath); !os.IsNotExist(err) {
		fmt.Printf("  Loading addressbook from: %s\n", abpath)
		err = ctx.ID.Contacts.ImportFrom(abpath)
		if err != nil {
			fmt.Println("  Loading addressbook failed!")
			log.Fatal(err)
		}
	}
	
	// let the session begin
	fmt.Println("  Starting session")
	sendMsgChan, receiveMsgChan, err := ctx.Run()
	if err != nil {
		log.Fatal(err)
	}
	
	return tr, tid, ctx, receiveMsgChan, sendMsgChan
}


func sendTestMsg(tr o3.ThreemaRest, abpath string, rid string, testMsg string, ctx o3.SessionContext, sendMsgChan chan<- o3.Message) {
	// check if we know the remote ID for
	// (just demonstration purposes \bc sending and receiving functions do this lookup for us)
	if rid != "" {
		if _, b := ctx.ID.Contacts.Get(rid); b == false {
			//retrieve the ID from Threema's servers
			myID := o3.NewIDString(rid)
			fmt.Printf("  Retrieving %s from directory server\n", myID.String())
			myContact, err := tr.GetContactByID(myID)
			if err != nil {
				log.Fatal(err)
			}
			// add them to our address book
			ctx.ID.Contacts.Add(myContact)
			
			//and save the address book
			fmt.Printf("  Saving addressbook to: %s\n", abpath)
			err = ctx.ID.Contacts.SaveTo(abpath)
			if err != nil {
				fmt.Println("  Saving addressbook failed!")
				log.Fatal(err)
			}
		}
	}
	
	// send our initial message to our recipient
	if rid != "" {
		fmt.Println("  Sending initial message to " + rid + ": " + testMsg)
		err := ctx.SendTextMessage(rid, testMsg, sendMsgChan)
		if err != nil {
			log.Fatal(err)
		}
	}
}


func receiveLoop(tid o3.ThreemaID, ctx o3.SessionContext, receiveMsgChan <-chan o3.ReceivedMsg, sendMsgChan chan<- o3.Message) {
	// handle incoming messages
	for receivedMessage := range receiveMsgChan {
		if receivedMessage.Err != nil {
			fmt.Printf("  Error Receiving Message: %s\n", receivedMessage.Err)
			continue
		}
		switch msg := receivedMessage.Msg.(type) {
		case o3.ImageMessage:
			// display the image if you like
		case o3.AudioMessage:
			// play the audio if you like
		case o3.TextMessage:
			// respond with a quote of what was send to us.
			fmt.Printf("Message from %s: %s\n--------------------\n\n", msg.Sender(), msg.Text())
			
			// but only if it's no a message we sent to ourselves, avoid recursive neverending qoutes
			if (tid.String() == msg.Sender().String()) {
				continue
			}
			
			// to make the quote render nicely in the app we use "markdown"
			// of the form "> PERSONWEQUOTE: Text of qoute\nSomething we wanna add."
			qoute := fmt.Sprintf("> %s: %s\n%s", msg.Sender(), msg.Text(), "Exactly!")
			// we use the convinient "SendTextMessage" function to send
			err := ctx.SendTextMessage(msg.Sender().String(), qoute, sendMsgChan)
			if err != nil {
				log.Fatal(err)
			}
			confirmMsg(ctx, msg, sendMsgChan)
			// give a thumbs up
			upm, err := o3.NewDeliveryReceiptMessage(&ctx, msg.Sender().String(), msg.ID(), o3.MSGAPPROVED)
			if err != nil {
				log.Fatal(err)
			}
			sendMsgChan <- upm
		case o3.GroupTextMessage:
			fmt.Printf("  %s for Group [%x] created by [%s]:\n%s\n", msg.Sender(), msg.GroupID(), msg.GroupCreator(), msg.Text())
		case o3.GroupManageSetNameMessage:
			fmt.Printf("  Group [%x] is now called %s\n", msg.GroupID(), msg.Name())
		case o3.GroupManageSetMembersMessage:
			fmt.Printf("  Group [%x] now includes %v\n", msg.GroupID(), msg.Members())
		case o3.GroupMemberLeftMessage:
			fmt.Printf("  Member [%s] left the Group [%x]\n", msg.Sender(), msg.GroupID())
		case o3.DeliveryReceiptMessage:
			fmt.Printf("  Message [%x] has been acknowledged by the server.\n", msg.MsgID())
		case o3.TypingNotificationMessage:
			fmt.Printf("  Typing Notification from %s: [%x]\n", msg.Sender(), msg.OnOff)
		default:
			fmt.Printf("  Unknown message type from: %s\nContent: %#v", msg.Sender(), msg)
		}
	}
}


func sendLoop(tr o3.ThreemaRest, abpath string, ctx o3.SessionContext, sendMsgChan chan<- o3.Message) {
	reader := bufio.NewReader(os.Stdin)
	
	fmt.Println("  Sending thread startet! Send messages via: ---ID---MESSAGE (e.g. 1337ABCDHello World!)")
	for {
		rid_msg, _ := reader.ReadString('\n')
		idValid := false
		if len(rid_msg) >= 8 {
			rid := rid_msg[0:8]
			msg := rid_msg[8:len(rid_msg)-1]
			
			ridRegex := regexp.MustCompile("\\A[0-9A-Z]{8}\\z")
			rid = ridRegex.FindString(strings.ToUpper(rid))
			if rid != "" && len(rid) == 8 {
				idValid = true
				
				if _, b := ctx.ID.Contacts.Get(rid); b == false {
					//retrieve the ID from Threema's servers
					myID := o3.NewIDString(rid)
					fmt.Printf("  Retrieving %s from directory server\n", myID.String())
					myContact, err := tr.GetContactByID(myID)
					if err != nil {
						log.Fatal(err)
					}
					// add them to our address book
					ctx.ID.Contacts.Add(myContact)
					
					//and save the address book
					fmt.Printf("  Saving addressbook to: %s\n", abpath)
					err = ctx.ID.Contacts.SaveTo(abpath)
					if err != nil {
						fmt.Println("  Saving addressbook failed!")
						log.Fatal(err)
					}
				}
				
				fmt.Println("  Sending message to " + rid + ".")
				err := ctx.SendTextMessage(rid, msg, sendMsgChan)
				if err != nil {
					log.Fatal(err)
				}
			}
		}
		
		if !idValid {
			fmt.Println("  ID is invalid!")
		}
	}
}


func confirmMsg(ctx o3.SessionContext, msg o3.TextMessage, sendMsgChan chan<- o3.Message) {
	// confirm to the sender that we received the message
	// this is how one can send messages manually without helper functions like "SendTextMessage"
	drm, err := o3.NewDeliveryReceiptMessage(&ctx, msg.Sender().String(), msg.ID(), o3.MSGDELIVERED)
	if err != nil {
		log.Fatal(err)
	}
	sendMsgChan <- drm
}
