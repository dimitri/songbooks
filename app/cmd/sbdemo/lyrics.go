package main

// Invented lyrics, written for the demo: a few songs get them so the
// lyrics views have something to show. Plain lines, no chords.

var verses = [][]string{
	{"Walking down the quiet road", "with the lanterns burning low", "every window has a story", "that the morning will not know"},
	{"Coffee cooling on the table", "and the radio half on", "you were humming something simple", "I was humming something wrong"},
	{"Il pleut sur le quai du canal", "les péniches dorment en rang", "je compte les ponts jusqu'à toi", "et la nuit compte avec moi"},
	{"Paper boats along the gutter", "racing to the harbour wall", "we were kings of every puddle", "we were never cold at all"},
	{"Le dernier tram est parti", "sans moi, sans bruit, sans regret", "je rentre à pied par les Lilas", "en sifflant ce qu'on chantait"},
	{"Postcards from a summer town", "with the colours fading out", "I keep them in a biscuit tin", "that I never talk about"},
	{"On the night bus to the river", "every seat a different dream", "someone's laughing at the back", "and the glass is full of steam"},
	{"Sur la table un vieux carnet", "des accords et des ratures", "trois chansons qu'on n'a pas finies", "pour des saisons plus mûres"},
	{"Seagulls arguing at sunrise", "over nothing but the wind", "I put the kettle on again", "and let the day begin"},
	{"Une fenêtre sur la cour", "un piano qui se trompe", "le voisin reprend la mesure", "et le silence nous rattrape"},
}

var choruses = [][]string{
	{"Oh, hold the light a little longer", "we are almost home", "hold the light a little longer", "we are not alone"},
	{"Et on danse, et on danse", "sous les lampions du soir", "et on danse, et on danse", "jusqu'à ne plus rien voir"},
	{"Sing it slow, sing it low", "let the river carry it on", "sing it slow, sing it low", "till the evening's gone"},
	{"Reste encore un peu", "la marée n'est pas pressée", "reste encore un peu", "la chanson n'est pas finie"},
}

var bridges = [][]string{
	{"And if the road turns back on itself", "we'll take the long way round"},
	{"Si demain la ville s'éteint", "on gardera la mélodie"},
	{"Count the stars between the cranes", "one for every song we know"},
}

// demoLyrics gives song n's lines for its i-th sung section of kind:
// the same chorus every time, verses in turn.
func demoLyrics(n int) func(kind string, i int) []string {
	verse := 0
	return func(kind string, i int) []string {
		switch kind {
		case "chorus", "post-chorus":
			return choruses[n%len(choruses)]
		case "bridge":
			return bridges[n%len(bridges)]
		case "pre-chorus":
			return verses[(n*3+7)%len(verses)][:2]
		}
		v := verses[(n*3+verse)%len(verses)]
		verse++
		return v
	}
}
