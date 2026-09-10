#include "catalog.hpp"
#include <stdexcept>
void Catalog::set_price(const std::string& sku,std::int64_t cents) {
    if(cents<0)throw std::invalid_argument("negative price");
    prices_[sku]=cents;
    ++revision_;
}
std::int64_t Catalog::price(const std::string& sku)const {return prices_.at(sku);}
