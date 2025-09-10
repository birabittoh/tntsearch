# TNTSearch

## Instructions

First, make sure to clone everything correctly:
```
git clone https://github.com/birabittoh/tntsearch
cd tntsearch
git submodule update --init
```

Then, simply run:
```
docker compose up --detach --build
```

## Environment

You can set the following variables in `.env`:
* `DB_PATH`: the path of the SQLite database;
* `CSV_PATH`: the path of the TNTVillage CSV dump;
* `PORT`: the address the app should listen on.


## License

TNTSearch is licensed under MIT.
