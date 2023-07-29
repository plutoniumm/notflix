type uuid = string;
type Emoji = string;

type Image = URL | Emoji;

interface User {
  name: string;
  image: Image;
};

interface Message {
  id: number;
  text: string;
  user: User;
};