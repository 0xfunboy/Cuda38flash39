#include <cstdlib>
#include <iostream>
#include <stdexcept>
#include <string>
#define CHECK(x) do { if (!(x)) { std::cerr << "FAIL line " << __LINE__ << ": " << #x << '\n'; return 1; } } while (0)
template<class F> bool invalid(F fn) { try { fn(); } catch (const std::invalid_argument&) { return true; } return false; }
#include "inventory.hpp"
int main() {
    Inventory inv{{"apple",4}}; import_inventory(inv,"apple,2\npear,0003\n"); CHECK(inv.at("apple")==6&&inv.at("pear")==3);
    const Inventory original=inv;
    for(auto bad:{"new,7\nbad,-1", "new,7\nbad,2x", "new,7\n", "bad name,1", "x,1000001", "x,999999999999999999999", "x,+1", "x, 1", "x,1\n\n"}) {
        // The single trailing LF case is valid and is checked independently below.
        if(std::string(bad)=="new,7\n")continue;
        inv=original; CHECK(invalid([&]{import_inventory(inv,bad);})); CHECK(inv==original);
    }
    inv={{"x",999999}}; CHECK(invalid([&]{import_inventory(inv,"y,5\nx,2");})); CHECK((inv==Inventory{{"x",999999}}));
    import_inventory(inv,""); CHECK(inv.at("x")==999999);
    import_inventory(inv,"new,7\n");CHECK(inv.at("new")==7);
    std::cout<<"PASS inventory-import: 12 scenarios\n";
}
