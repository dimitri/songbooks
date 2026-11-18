    // Songbooks: join muted; ?headphones=1, the voice unprocessed.
    updateSettings({localMute: true});
    if(parms.get('headphones') === '1')
        updateSettings({preprocessing: false, hqaudio: true});
    else if(parms.get('headphones') === '0')
        updateSettings({preprocessing: true, hqaudio: false});
