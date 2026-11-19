package main

import (
	"fmt"
	"strings"

	"app/internal/brand"
)

// Every name sbdemo writes comes from the lists below, picked by a
// number (a row's id, its rank): the same database gives the same demo
// every time. All of them are invented; none is meant to be anyone's.

var firstNames = []string{
	"Camille", "Hugo", "Léa", "Thomas", "Lucie", "Nathan", "Manon", "Julien",
	"Emma", "Antoine", "Clara", "Maxime", "Jade", "Romain", "Zoé", "Bastien",
	"Margaux", "Quentin", "Élise", "Mathis", "Oliver", "Grace", "Harry", "Amelia",
	"George", "Isla", "Jack", "Poppy", "Noah", "Ruby", "Leo", "Freya",
	"Arthur", "Lily", "Oscar", "Ivy", "Samuel", "Hazel", "Owen", "Maeve",
}

var lastNames = []string{
	"Lefèvre", "Garnier", "Rousseau", "Faure", "Chevalier", "Perrin", "Morel", "Fontaine",
	"Gauthier", "Bonnet", "Lambert", "Girard", "Renaud", "Brun", "Colin", "Vidal",
	"Leclerc", "Aubert", "Barbier", "Picard", "Hartley", "Whitmore", "Ashby", "Fenwick",
	"Holloway", "Pemberton", "Caldwell", "Thornton", "Langley", "Merriweather", "Stanton", "Blackwood",
	"Ellison", "Kendrick", "Radcliffe", "Sutton", "Hayward", "Prescott", "Winslow", "Garrick",
}

// The names given on purpose: the local dev account and Studio Lumen's
// coach. Neither list above can make them (no Mercier, no Marchal).
const (
	lucasName = "Lucas Mercier"
	coachName = "Inès Marchal"
)

// pick walks a set of n combinations in a fixed, scrambled order: the
// i-th of them, distinct for every i below n. stride must be coprime with
// n for that, which the callers' tests check.
func pick(i, n, stride int) int {
	if i < 0 {
		i = -i
	}
	return (i*stride + i/n) % n
}

// PersonName is the i-th invented person's name, "First Last".
func PersonName(i int) string {
	n := len(firstNames) * len(lastNames)
	p := (i % n * 37) % n // 37 is coprime with 1600
	return firstNames[p%len(firstNames)] + " " + lastNames[p/len(firstNames)]
}

// The parts invented song titles are made of.
var (
	titleColors = []string{
		"Cobalt", "Amber", "Silver", "Indigo", "Crimson", "Velvet", "Copper", "Ivory",
		"Scarlet", "Hazel", "Golden", "Pale", "Emerald", "Saffron", "Slate", "Ochre",
		"Violet", "Rusty", "Faded", "Electric",
	}
	titleTimes = []string{
		"Morning", "Evening", "Sunday", "Midnight", "Harbour", "Highway", "Season", "Weather",
		"Avenue", "Horizon", "Window", "Summer", "Winter", "Shoreline", "Daylight", "Lullaby",
		"Parade", "Echo", "Garden", "Signal",
	}
	titleThings = []string{
		"Night Train", "Paper Boat", "Last Ferry", "Slow Bus", "Blue Bicycle", "Old Tram", "Red Kite", "Night Bus",
		"Small Plane", "Sailing Boat", "Postcard", "Long Letter", "Late Call", "Rowing Boat", "River Barge", "Night Coach",
		"Tin Soldier", "Lost Balloon", "Morning Mail", "Quiet Song",
	}
	titlePlaces = []string{
		"Arles", "Brighton", "Saint-Malo", "Lisbon", "Dover", "Nantes", "Galway", "Biarritz",
		"Aberdeen", "Sète", "Valparaíso", "Honfleur", "Inverness", "Collioure", "Bergen", "Cork",
		"Trieste", "Étretat", "Whitby", "Menton",
	}
	titlePlurals = []string{
		"Lanterns", "Sparrows", "Candles", "Footsteps", "Paper Stars", "Seagulls", "Fireflies", "Raindrops",
		"Pebbles", "Small Hours", "Kites", "Streetlights", "Shadows", "Old Songs", "Lighthouses", "Wild Roses",
		"Echoes", "Strangers", "Bonfires", "Promises",
	}
	titleMoments = []string{
		"Low Tide", "Dawn", "Dusk", "the Crossroads", "the Station", "the Pier", "the Border", "Closing Time",
		"First Light", "the Fair", "the Corner", "High Noon", "the Gate", "the Harbour", "Daybreak", "the Bridge",
		"the Market", "Last Orders", "Sundown", "the Lighthouse",
	}
	titleFrNouns = []string{
		"La Valse", "Le Bal", "La Ballade", "Les Lumières", "Le Café", "La Chanson", "Les Rêves", "Le Quai",
		"La Marée", "Les Nuits", "Le Jardin", "La Fanfare", "Le Manège", "Les Saisons", "La Fenêtre", "Le Phare",
		"La Route", "Les Voiles", "Le Petit Matin", "La Dernière Danse",
	}
	titleFrPlaces = []string{
		"du Canal", "des Quais", "de Belleville", "du Port", "des Marins", "de la Gare", "du Dimanche", "des Collines",
		"de l'Estuaire", "du Faubourg", "des Halles", "de Minuit", "du Square", "des Lilas", "de la Rive", "du Vieux Port",
		"des Tilleuls", "de l'Hiver", "du Soir", "des Brumes",
	}
)

// SongTitle is the i-th invented song title: "Cobalt Morning", "Night
// Train to Arles", "Lanterns at Low Tide", "La Valse du Canal"… four
// shapes in turn, 1600 titles before one comes back.
func SongTitle(i int) string {
	if i < 0 {
		i = -i
	}
	j := i / 4
	switch i % 4 {
	case 0:
		p := pick(j, 400, 151)
		return titleColors[p%20] + " " + titleTimes[p/20]
	case 1:
		p := pick(j, 400, 263)
		return titleThings[p%20] + " to " + titlePlaces[p/20]
	case 2:
		p := pick(j, 400, 97)
		return titlePlurals[p%20] + " at " + titleMoments[p/20]
	default:
		p := pick(j, 400, 211)
		return titleFrNouns[p%20] + " " + titleFrPlaces[p/20]
	}
}

// Invented bands: the app's bands are named from this list, and invented
// songs are credited to them.
var bandNames = []string{
	"The Paper Lanterns", "Velvet Hour", "Nord Express", "Les Fanfarons", "Cobalt Avenue",
	"Marée Basse", "The Quiet Harbours", "Les Volets Bleus", "Saltwater Radio", "Orchestre du Lundi",
	"The Copper Kites", "Lune Rousse", "Midnight Ferry", "Les Sardines Électriques", "Hollow Pines",
	"Les Gens du Quai", "The Amber Days", "Café Mistral", "Northern Lanterns", "Les Petites Heures",
	"Silver Tramline", "Brume Légère", "The Late Arrivals", "Les Funambules", "Ivory Coastline",
	"Le Grand Manège", "Driftwood Choir", "Les Oiseaux de Nuit", "The Pale Riders Club", "Rive Droite",
	"Harbour Lights Trio", "Les Cousins d'Avril",
}

// BandName is the i-th invented band.
func BandName(i int) string {
	if i < 0 {
		i = -i
	}
	return bandNames[i%len(bandNames)]
}

// The four communities a production copy has, by name, and what each
// becomes. Any other is CommunityName's.
var knownCommunities = map[string]string{
	"Bands Camp":         "Jam du Canal",
	"Florent Passamonti": "Atelier Blues Rive Gauche",
	"Chloé Paul":         "Studio Lumen",
	"JamSpace":           "Salle Bleue Jam",
}

var otherCommunities = []string{
	"Collectif Les Ondes", "Chorale du Faubourg", "Atelier Cordes Sensibles", "Club Swing des Lilas",
	"École du Vieux Port", "Les Ateliers du Square", "Big Band des Halles", "Studio Contrepoint",
}

// CommunityName is what a community called name becomes; i (its id) picks
// an invented one for a community sbdemo doesn't know.
func CommunityName(name string, i int) string {
	if n, ok := knownCommunities[name]; ok {
		return n
	}
	for _, n := range knownCommunities {
		if n == name {
			return n // already renamed
		}
	}
	if i < 0 {
		i = -i
	}
	return otherCommunities[i%len(otherCommunities)]
}

// Community looks, by new name; communityLooks for the others.
var namedLooks = map[string]brand.Brand{
	// A canal at night: deep teal header, warm orange accent.
	"Jam du Canal": {Background: "#f2f5f4", Surface: "#ffffff", Text: "#13201f", Muted: "#4a5a58",
		Accent: "#b4450f", Accent2: "#f3b37a", Header: "#123c3a",
		FontDisplay: "bebas", FontUI: "inter", Shape: "soft", Mark: "JC",
		Tagline: "Le vendredi, on branche les amplis au bord de l'eau"},
	// Left bank blues: black and cream, a brick red.
	"Atelier Blues Rive Gauche": {Background: "#fbf8f2", Surface: "#ffffff", Text: "#161311", Muted: "#5d554d",
		Accent: "#9b2c1f", Accent2: "#e8a87c", Header: "#1b1715",
		FontDisplay: "oswald", FontUI: "work-sans", Shape: "square", Mark: "AB",
		Tagline: "Guitare blues, pas à pas, à votre rythme"},
	// Light and air: pale lilac, deep plum, a gold touch.
	"Studio Lumen": {Background: "#f6f3f8", Surface: "#ffffff", Text: "#1f1a24", Muted: "#5a5163",
		Accent: "#5b2a86", Accent2: "#e2c275", Header: "#2a1f33",
		FontDisplay: "cormorant", FontUI: "montserrat", Shape: "pill", Mark: "SL",
		Tagline: "Coaching vocal, en studio et en ligne"},
	// A blue room: navy and sky blue.
	"Salle Bleue Jam": {Background: "#f1f5fb", Surface: "#ffffff", Text: "#0f1a2b", Muted: "#4b5a70",
		Accent: "#1d4ed8", Accent2: "#9cc3ff", Header: "#0e2247",
		FontDisplay: "space-grotesk", FontUI: "nunito-sans", Shape: "soft", Mark: "SB",
		Tagline: "Des jams ouvertes à tous les niveaux"},
}

// palettes are invented looks for everything else (bands, other
// communities), with a tagline each.
var palettes = []brand.Brand{
	{Background: "#faf6ef", Surface: "#ffffff", Text: "#211a12", Muted: "#615548", Accent: "#8a4b0f", Accent2: "#f0c27b", Header: "#2b2118", FontDisplay: "fraunces", FontUI: "inter", Shape: "soft"},
	{Background: "#eef3f1", Surface: "#ffffff", Text: "#10201b", Muted: "#4d5f59", Accent: "#0f6b55", Accent2: "#9fe0c9", Header: "#0f2a24", FontDisplay: "space-grotesk", FontUI: "work-sans", Shape: "pill"},
	{Background: "#16121c", Surface: "#221c2b", Text: "#f3eef8", Muted: "#b9aec6", Accent: "#d9467a", Accent2: "#f6a6c1", Header: "#0d0a11", FontDisplay: "anton", FontUI: "inter", Shape: "square"},
	{Background: "#f4f4f1", Surface: "#ffffff", Text: "#1a1a17", Muted: "#55554f", Accent: "#3b5bdb", Accent2: "#b7c6ff", Header: "#20232e", FontDisplay: "playfair", FontUI: "nunito-sans", Shape: "soft"},
	{Background: "#fdf5f2", Surface: "#ffffff", Text: "#2a1612", Muted: "#6b4f48", Accent: "#b23a1d", Accent2: "#ffb199", Header: "#3a1a12", FontDisplay: "oswald", FontUI: "assistant", Shape: "square"},
	{Background: "#0f1720", Surface: "#17222e", Text: "#e9f1f8", Muted: "#a7b7c6", Accent: "#2f9e8f", Accent2: "#8fd6cb", Header: "#0a1016", FontDisplay: "bebas", FontUI: "work-sans", Shape: "soft"},
	{Background: "#f7f3fb", Surface: "#ffffff", Text: "#1e1726", Muted: "#5c5068", Accent: "#7a3fb0", Accent2: "#d9b8f5", Header: "#251a30", FontDisplay: "eb-garamond", FontUI: "inter", Shape: "pill"},
	{Background: "#f5f7ec", Surface: "#ffffff", Text: "#1b1f10", Muted: "#575e45", Accent: "#55701a", Accent2: "#cfe39a", Header: "#232a12", FontDisplay: "fraunces", FontUI: "nunito-sans", Shape: "soft"},
}

var bandTaglines = []string{
	"Reprises pop-rock pour les soirs d'été",
	"Chansons originales, guitares claires",
	"Folk acoustique et harmonies à trois voix",
	"Rock garage, répétitions du lundi",
	"Soul, funk et cuivres imaginaires",
	"Des valses, des balades, un accordéon",
	"Blues électrique, du vendredi au dimanche",
	"Pop en français, mélodies au piano",
}

// Look is the i-th invented look, its mark from name's initials.
func Look(name string, i int) brand.Brand {
	if b, ok := namedLooks[name]; ok {
		return b
	}
	if i < 0 {
		i = -i
	}
	b := palettes[i%len(palettes)]
	b.Mark = Initials(name)
	b.Tagline = bandTaglines[i%len(bandTaglines)]
	return b
}

// Initials are two letters for a mark: the first letters of a name's
// first two words that count ("The Paper Lanterns" → "PL").
func Initials(name string) string {
	var out []rune
	for _, w := range strings.Fields(name) {
		switch strings.ToLower(w) {
		case "the", "les", "le", "la", "du", "de", "des", "d'", "&":
			continue
		}
		r := []rune(w)
		out = append(out, []rune(strings.ToUpper(string(r[0])))...)
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 1 {
		// One word that counts: its first two letters.
		for _, w := range strings.Fields(name) {
			if r := []rune(strings.ToUpper(w)); r[0] == out[0] && len(r) > 1 {
				out = append(out, r[1])
				break
			}
		}
	}
	return string(out)
}

// Florent's two methods become volumes of one.
func MethodName(i int) string { return fmt.Sprintf("Méthode Blues, volume %d", i+1) }

// Lesson titles: technique, the way a guitar course names them.
var lessonTitles = []string{
	"Mon premier shuffle", "Shuffle en la", "Gamme pentatonique, position 1", "Gamme pentatonique, position 2",
	"Le bend d'un ton", "Questions et réponses", "Le turnaround en mi", "Walking bass sur douze mesures",
	"Le slide, première approche", "Rythmique en accords de neuvième", "Le blues mineur", "Arpèges de septième",
	"Le vibrato", "La note bleue", "Shuffle en mi, à la basse", "Double stops en tierces",
	"Le blues lent en sol", "Phrasé : laisser respirer", "Riffs en boogie", "Gamme mixolydienne",
	"Le rock'n'roll en la", "Plan de conclusion", "Le blues jazz : II-V-I", "Le bend et relâche",
	"Picking hybride", "Le blues en open de ré", "Accords à trois sons", "Solo facile en la",
	"Accompagner un chanteur", "Construire un solo", "Le blues rapide", "Rythmique funky",
	"La gamme blues complète", "Improviser sur un riff", "Révisions et morceau final",
}

// LessonTitle is the n-th lesson's title (n from 1).
func LessonTitle(n int) string {
	if n < 1 {
		n = 1
	}
	return lessonTitles[(n-1)%len(lessonTitles)]
}

// Venues, all invented.
var venues = []string{
	"Le Quai 9", "Studio Mistral", "La Péniche Rouge", "Le Comptoir des Lilas", "Salle Bastide",
	"Le Petit Faubourg", "La Cave à Riffs", "Studio Bleu Nuit", "Le Kiosque", "La Grange aux Sons",
}

// Venue is the i-th invented venue.
func Venue(i int) string {
	if i < 0 {
		i = -i
	}
	return venues[i%len(venues)]
}

// Songbook names that say nothing about anyone and stay.
var genericSongbooks = map[string]bool{
	"My songbook": true, "Songbook": true, "Repertoire": true, "Former repertoire": true,
	"Mon recueil": true, "Recueil": true, "Répertoire": true, "Ancien répertoire": true,
}

var songbookNames = []string{
	"Répertoire d'automne", "Set acoustique", "Standards du mardi", "Nouveautés à travailler",
	"Ballades", "Pour le concert", "Les classiques", "Chansons à trois voix",
}

// SongbookName is the i-th invented songbook name.
func SongbookName(i int) string {
	if i < 0 {
		i = -i
	}
	return songbookNames[i%len(songbookNames)]
}

var setlistTitles = []string{
	"Set d'automne", "Concert du samedi", "Set acoustique", "Soirée au Quai 9",
	"Fête de la musique", "Set court, 30 minutes", "Concert de printemps", "Set électrique",
}

// SetlistTitle is the i-th invented set list title.
func SetlistTitle(i int) string {
	if i < 0 {
		i = -i
	}
	return setlistTitles[i%len(setlistTitles)]
}

// Teaching remarks a coach might write, invented.
var teachingNotes = []string{
	"Très bon travail sur le soutien du souffle. À la maison : les sirènes sur « ou », 5 minutes par jour.",
	"Le passage vers la voix mixte se fait plus en douceur. Garder la mâchoire libre sur les voyelles ouvertes.",
	"Justesse en progrès sur les fins de phrase. Travailler le refrain un demi-ton plus bas d'abord.",
	"Belle présence, attention à ne pas pousser dans les aigus. Reprendre l'exercice des lip trills.",
	"Le placement rythmique du couplet est à reprendre avec le métronome à 80.",
	"On a travaillé la respiration costale. Le legato des phrases longues s'améliore nettement.",
	"Voix de tête plus stable. Prochaine séance : nuances, du piano au mezzo-forte.",
	"Bonne écoute du groupe, les harmonies tiennent. Préparer la deuxième voix du pont.",
	"Diction à préciser sur les consonnes finales. Lire le texte à voix haute avant de chanter.",
	"Échauffement trop court aujourd'hui : prévoir dix minutes avant chaque répétition.",
}

// TeachingNote is the i-th invented teaching remark.
func TeachingNote(i int) string {
	if i < 0 {
		i = -i
	}
	return teachingNotes[i%len(teachingNotes)]
}

var studentProfiles = []string{
	"Chante depuis deux ans en chorale. Objectif : chanter seule sur scène.",
	"Guitariste qui veut assurer les chœurs dans son groupe.",
	"Reprend le chant après une longue pause. Aime la soul et le jazz vocal.",
	"Prépare une audition pour une comédie musicale au printemps.",
	"Voix naturellement puissante, travaille la nuance et le contrôle.",
	"Chante en groupe de reprises pop-rock, veut gagner en endurance.",
}

// StudentProfile is the i-th coach's line about a student.
func StudentProfile(i int) string {
	if i < 0 {
		i = -i
	}
	return studentProfiles[i%len(studentProfiles)]
}
