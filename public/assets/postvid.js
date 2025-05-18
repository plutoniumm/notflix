let player;
let next = null;
videojs( "my-video" ).ready( function () {
  console.log( `[INFO] Player ready!\nAutoplay: ${ autoplay }` );
  player = this;
  next = null;
  if ( autoplay ) player.play();

  let lastPlayed = localStorage.getItem( 'lastPlayed' );
  lastPlayed = JSON.parse( lastPlayed || "{}" );
  if ( lastPlayed[ video ] ) {
    player.currentTime( lastPlayed[ video ] );
    $( 'video' )
  } else {
    player.currentTime( 0 );
    lastPlayed[ video ] = 0;
  }

  let lastTime = 0;
  setInterval( () => {
    lastPlayed[ video ] = player.currentTime();
    localStorage.setItem( 'lastPlayed', JSON.stringify( lastPlayed ) );

    if ( player.paused() ) return;
    if ( player.currentTime() === lastTime ) {
      player.currentTime( player.currentTime() );
    };

    lastTime = player.currentTime();
    const dur = player.duration();
    if (
      ( lastTime * 100 / dur >= 95 ) || ( dur - lastTime <= 30 )
    ) {
      console.log( `[INFO] Video is almost finished` );
      let idx = videoList.findIndex( e => ( e[ 0 ] === video ) );

      if ( idx === videoList.length - 1 )
        return console.log( `[INFO] Last video` );
      nextBtn.href = videoList[ idx + 1 ][ 1 ];
      nextBtn.classList.remove( 'hidden' );

      next = nextBtn.href + ( autoplay ? '&autoplay=1' : '' );
      setTimeout( () => window.location.href = next, 5e3 );
    };
  }, 2000 );

  addHotkeys( player );
  trySubs( video );
} );

async function trySubs ( video ) {
  let subfile = video.replace( '.mp4', '.vtt' );
  const isSub = await exists( `/subs/${ subfile }` );
  if ( !isSub ) return;

  player.addRemoteTextTrack( {
    kind: 'captions',
    src: `/subs/${ subfile }`,
    srclang: 'en',
    label: 'notflix-sub',
    default: true
  } );

  let tracks = player.remoteTextTracks();
  for ( let i = 0;i < tracks.length;i++ ) {
    tracks[ i ].mode =
      tracks[ i ].label === 'notflix-sub' ? 'showing' : 'disabled';
  }
}

function addHotkeys ( player ) {
  document.addEventListener( 'keydown', ( event ) => {
    console.log( `[KEY] ${ event.which }`, player.currentTime() );
    ///* +TIME */
    if ( event.which === 39 && event.shiftKey ) { // +30s
      player.currentTime( player.currentTime() + 30 );
    } else if ( event.which === 39 && event.altKey ) { // +100ms
      player.currentTime( player.currentTime() + 0.1 );
    } else if ( event.which === 39 ) { // +5s
      player.currentTime( player.currentTime() + 5 );
      /* -TIME */
    } else if ( event.which === 37 && event.shiftKey ) { // +30s
      player.currentTime( player.currentTime() - 30 );
    } else if ( event.which === 37 && event.altKey ) { // +100ms
      player.currentTime( player.currentTime() - 0.1 );
    } else if ( event.which === 37 ) { // -5s
      player.currentTime( player.currentTime() - 5 );
      /* OTHERS */
    } else if ( event.which === 32 ) { // play/pause
      player.paused() ? player.play() : player.pause();
    } else if ( event.which === 77 ) { // mute/unmute
      player.muted() ? player.muted( false ) : player.muted( true );
    } else if ( event.which === 70 ) { // fullscreen
      player.isFullscreen() ? player.exitFullscreen() : player.requestFullscreen();
    } else if ( event.which >= 48 && event.which <= 57 ) { // (0-9)0%
      let number = event.which - 48;
      player.currentTime( player.duration() * number * 0.1 );
    } else if ( event.which === 78 ) { // n
      if ( next ) {
        window.location.href = next;
      };
    }
  } );
};

window.addEventListener( 'keydown', ( e ) => {
  const active = document.activeElement.tagName;

  if ( e.key === 'l' ) {
    if ( [ 'NF-LIST', 'INPUT' ].includes( active ) ) return;
    listings.classList.toggle( 'hidden' );
  };
} );