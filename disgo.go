package main

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/bwmarrin/discordgo"
	_ "github.com/mattn/go-sqlite3"
	"math/rand"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Command func(string, string, []string) (string, error)
type KarmaDto struct {
	UserId string
	Karma  int64
}

const OWN_USER_ID = "160807650345353226"

var sqlClient *sql.DB
var voteTime map[string]time.Time = make(map[string]time.Time)
var userIdRegex = regexp.MustCompile(`<@(\d+?)>`)

func twitch(chanId, authorId string, args []string) (string, error) {
	if len(args) < 1 {
		cmd := exec.Command("find", "-iname", "*_nolink")
		cmd.Dir = "/home/ross/markov/"
		out, err := cmd.Output()
		if err != nil {
			return "", err
		}
		files := strings.Fields(string(out))
		for i := range files {
			files[i] = strings.Replace(files[i], "./", "", 1)
			files[i] = strings.Replace(files[i], "_nolink", "", 1)
		}
		return strings.Join(files, ", "), nil
	}
	cmd := exec.Command("/home/ross/markov/1-markov.out", "1")
	logs, err := os.Open("/home/ross/markov/" + args[0] + "_nolink")
	if err != nil {
		return "", err
	}
	cmd.Stdin = logs
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func soda(chanId, authorId string, args []string) (string, error) {
	return twitch(chanId, authorId, []string{"sodapoppin"})
}

func lirik(chanId, authorId string, args []string) (string, error) {
	return twitch(chanId, authorId, []string{"lirik"})
}

func forsen(chanId, authorId string, args []string) (string, error) {
	return twitch(chanId, authorId, []string{"forsenlol"})
}

func vote(chanId, authorId string, args []string, inc int64) (string, error) {
	if len(args) < 1 {
		return "", errors.New("No userId provided")
	}
	userMention := args[0]
	var userId string
	if match := userIdRegex.FindStringSubmatch(userMention); match != nil {
		userId = match[1]
	} else {
		return "", errors.New("No valid mention found")
	}
	if authorId != OWN_USER_ID {
		lastVoteTime, validTime := voteTime[authorId]
		if validTime && time.Since(lastVoteTime).Minutes() < 5 {
			return "Slow down champ.", nil
		}
	}
	if authorId == userId {
		if inc > 0 {
			_, err := vote(chanId, OWN_USER_ID, []string{"<@" + authorId + ">"}, -1)
			if err != nil {
				return "", err
			}
			voteTime[authorId] = time.Now()
		}
		return "No.", nil
	}

	var karma int64
	err := sqlClient.QueryRow("select Karma from karma where ChanId = ? and UserId = ?", chanId, userId).Scan(&karma)
	if err != nil {
		if err == sql.ErrNoRows {
			karma = 0
			_, insertErr := sqlClient.Exec("insert into Karma(ChanId, UserId, Karma) values (?, ?, ?)", chanId, userId, karma)
			if insertErr != nil {
				return "", err
			}
		} else {
			return "", err
		}
	}

	karma += inc
	_, err = sqlClient.Exec("update karma set Karma = ? where ChanId = ? and UserId = ?", karma, chanId, userId)
	if err != nil {
		return "", err
	}

	voteTime[authorId] = time.Now()
	return "", nil
}

func upvote(chanId, authorId string, args []string) (string, error) {
	return vote(chanId, authorId, args, 1)
}

func downvote(chanId, authorId string, args []string) (string, error) {
	return vote(chanId, authorId, args, -1)
}

func votes(chanId, authorId string, args []string) (string, error) {
	if len(args) > 0 {
		var userId string
		fmt.Println(args[0])
		if match := userIdRegex.FindStringSubmatch(args[0]); match != nil {
			userId = match[1]
		} else {
			return "", errors.New("No valid mention found")
		}
		var karma int64
		err := sqlClient.QueryRow("select Karma from karma where ChanId = ? and UserId = ?", chanId, userId).Scan(&karma)
		if err != nil {
			return "", err
		}
		return strconv.FormatInt(karma, 10), nil
	} else {
		rows, err := sqlClient.Query("select UserId, Karma from karma where ChanId = ? order by Karma desc limit 5", chanId)
		if err != nil {
			return "", err
		}
		defer rows.Close()
		votes := make([]KarmaDto, 0)
		for rows.Next() {
			var userId string
			var karma int64
			err := rows.Scan(&userId, &karma)
			if err != nil {
				return "", err
			}
			votes = append(votes, KarmaDto{userId, karma})
		}
		finalString := ""
		for i, vote := range votes {
			if i >= 5 {
				break
			}
			finalString += fmt.Sprintf("<@%s>: %d, ", vote.UserId, vote.Karma)
		}
		if len(finalString) > 0 {
			return finalString[:len(finalString)-2], nil
		} else {
			return "", nil
		}
	}
}

func roll(chanId, authorId string, args []string) (string, error) {
	var max int
	if len(args) < 1 {
		max = 6
	} else {
		var err error
		max, err = strconv.Atoi(args[0])
		if err != nil || max < 0 {
			return "", err
		}
	}
	return strconv.Itoa(rand.Intn(max) + 1), nil
}

func uptime(chanId, authorId string, args []string) (string, error) {
	output, err := exec.Command("uptime").Output()
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(output)), nil
}

func help(chanId, authorId string, args []string) (string, error) {
	return "twitch [streamer (optional)], soda, lirik, forsen, roll [sides (optional)], upvote [@user] (or @user++), downvote [@user] (or @user--), karma/votes [@user (optional), uptime", nil
}

func makeMessageCreate() func(*discordgo.Session, *discordgo.MessageCreate) {
	regexes := []*regexp.Regexp{regexp.MustCompile(`^<@` + OWN_USER_ID + `>\s+(.+)`), regexp.MustCompile(`^\/(.+)`)}
	upvoteRegex := regexp.MustCompile(`(<@\d+?>)\s*\+\+`)
	downvoteRegex := regexp.MustCompile(`(<@\d+?>)\s*--`)
	funcMap := map[string]Command{
		"twitch":   Command(twitch),
		"soda":     Command(soda),
		"lirik":    Command(lirik),
		"forsen":   Command(forsen),
		"roll":     Command(roll),
		"help":     Command(help),
		"upvote":   Command(upvote),
		"downvote": Command(downvote),
		"votes":    Command(votes),
		"karma":    Command(votes),
		"uptime":   Command(uptime),
	}

	return func(s *discordgo.Session, m *discordgo.MessageCreate) {
		now := time.Now()
		fmt.Printf("%20s %20s %20s > %s\n", m.ChannelID, now.Format(time.Stamp), m.Author.Username, m.Content)
		_, err := sqlClient.Exec("INSERT INTO messages (ChanId, AuthorId, Timestamp, Message) values (?, ?, ?, ?)",
			m.ChannelID, m.Author.ID, now.Format(time.RFC3339Nano), m.Content)
		if err != nil {
			fmt.Println("ERROR inserting into messages db")
			fmt.Println(err.Error())
		}
		if m.Author.ID == OWN_USER_ID {
			return
		}
		var command []string
		if match := upvoteRegex.FindStringSubmatch(m.Content); match != nil {
			command = []string{"upvote", match[1]}
		}
		if len(command) == 0 {
			if match := downvoteRegex.FindStringSubmatch(m.Content); match != nil {
				command = []string{"downvote", match[1]}
			}
		}
		if len(command) == 0 {
			for _, regex := range regexes {
				if match := regex.FindStringSubmatch(m.Content); match != nil {
					command = strings.Fields(match[1])
					break
				}
			}
		}
		if len(command) == 0 {
			return
		}
		if cmd, valid := funcMap[strings.ToLower(command[0])]; valid {
			reply, err := cmd(m.ChannelID, m.Author.ID, command[1:])
			if err != nil {
				fmt.Println("ERROR in " + command[0])
				fmt.Printf("ARGS: %v\n", command[1:])
				fmt.Println("ERROR: " + err.Error())
				return
			}
			if len(reply) > 0 {
				s.ChannelMessageSend(m.ChannelID, reply)
			}
			return
		}
	}
}

func gameUpdater(s *discordgo.Session, ticker <-chan time.Time) {
	currentGame := ""
	games := []string{"Skynet Simulator 2020", "Kill All Humans", "WW III: The Game", "9gag Meme Generator", "Subreddit Simulator", "Runescape"}
	for {
		select {
		case <-ticker:
			if currentGame != "" {
				changeGame := rand.Intn(4)
				if changeGame != 0 {
					continue
				}
				currentGame = ""
			} else {
				index := rand.Intn(len(games) * 3)
				if index >= len(games) {
					currentGame = ""
				} else {
					currentGame = games[index]
				}
			}
			s.UpdateStatus(0, currentGame)
		}
	}
}

func main() {
	var err error
	sqlClient, err = sql.Open("sqlite3", "sqlite.db")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	client, err := discordgo.New(LOGIN_EMAIL, LOGIN_PASSWORD)
	if err != nil {
		fmt.Println(err)
		return
	}
	client.AddHandler(makeMessageCreate())
	client.Open()

	gameTicker := time.NewTicker(193 * time.Second)
	go gameUpdater(client, gameTicker.C)

	var input string
	fmt.Scanln(&input)
	return
}
